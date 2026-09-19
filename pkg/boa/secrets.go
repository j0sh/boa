package boa

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"reflect"
)

// secretFileEntry links a real file-path parameter to the secret parameter it
// populates. Both paths point at ordinary user-declared struct fields; the
// runtime state makes the resolver idempotent across the pre-validation hooks.
type secretFileEntry struct {
	secretPath fieldPath
	filePath   fieldPath
	secretName string
	fileName   string

	appliedPath  string
	appliedValue string
}

func hasFileTag(tags reflect.StructTag) bool {
	return tags.Get("file") == "true" || tags.Get("secretfor") != ""
}

// registerSecretFile validates and records a secretfor relationship. Target
// names deliberately resolve only among direct siblings in the declaring
// struct; promoted or qualified names would make relationships ambiguous.
func (ctx *processingContext) registerSecretFile(file parameter, targetName string) error {
	fileMeta, ok := file.(*paramMeta)
	if !ok {
		return fmt.Errorf("secretfor %q is only supported on struct fields", targetName)
	}
	fileIndices := splitPath(fileMeta.pathKey)
	if len(fileIndices) == 0 {
		return fmt.Errorf("secretfor %q has no declaring struct", targetName)
	}

	parentIndices := fileIndices[:len(fileIndices)-1]
	parent, ok := ctx.resolveFieldValue(joinPath(parentIndices))
	if !ok {
		return fmt.Errorf("secretfor %q has an invalid declaring struct", targetName)
	}
	parentType := reflect.Indirect(parent).Type()
	targetField, ok := parentType.FieldByName(targetName)
	if !ok || !targetField.IsExported() || len(targetField.Index) != 1 {
		return fmt.Errorf("secretfor target %q must name an exported field in the same struct", targetName)
	}

	fileName := parentType.Field(fileIndices[len(fileIndices)-1]).Name
	fileIndices[len(fileIndices)-1] = targetField.Index[0]
	targetPath := joinPath(fileIndices)
	if targetPath == fileMeta.pathKey {
		return fmt.Errorf("secretfor field %s cannot target itself", targetName)
	}
	if ctx.mirrorByPath[targetPath] == nil {
		return fmt.Errorf("secretfor target %q is not a BOA parameter", targetName)
	}
	if targetField.Tag.Get("secret") != "true" {
		return fmt.Errorf("secretfor target %q must have secret:\"true\"", targetName)
	}
	for _, entry := range ctx.SecretFiles {
		if entry.secretPath == targetPath {
			return fmt.Errorf("secret field %q has more than one secretfor companion", targetName)
		}
	}

	ctx.SecretFiles = append(ctx.SecretFiles, secretFileEntry{
		secretPath: targetPath,
		filePath:   fileMeta.pathKey,
		secretName: targetField.Name,
		fileName:   fileName,
	})
	return nil
}

// resolveSecretFiles applies every secretfor source. It is called both before
// and after PreValidate hooks. The applied state distinguishes the value this
// resolver injected from an independently supplied direct secret.
func resolveSecretFiles(ctx *processingContext) error {
	for i := range ctx.SecretFiles {
		entry := &ctx.SecretFiles[i]
		secret := ctx.mirrorByPath[entry.secretPath]
		file := ctx.mirrorByPath[entry.filePath]
		if secret == nil || file == nil || !file.HasValue() {
			continue
		}
		if !secret.IsEnabled() || !file.IsEnabled() || secret.IsIgnored() || file.IsIgnored() {
			continue
		}
		field, ok := ctx.resolveFieldValue(entry.secretPath)
		if !ok {
			continue
		}

		// Tags ensure string mirrors; an empty appliedPath means no successful read yet.
		path := reflect.Indirect(reflect.ValueOf(file.valuePtrF())).String()
		fileName := cmp.Or(file.GetName(), entry.fileName)
		if secret.HasValue() && (entry.appliedPath == "" || reflect.Indirect(reflect.ValueOf(secret.valuePtrF())).String() != entry.appliedValue) {
			return fmt.Errorf("secret %q and secret file %q cannot both be set", cmp.Or(secret.GetName(), entry.secretName), fileName)
		}
		if entry.appliedPath != "" && path == entry.appliedPath {
			continue
		}

		contents, err := readSecretFile(path)
		if err != nil {
			return fmt.Errorf("secret file %q: %w", fileName, err)
		}
		// Update the field and mirror together so sync cannot restore an old value.
		if field.Kind() == reflect.Pointer {
			field.Set(reflect.New(field.Type().Elem()))
			field = field.Elem()
		}
		field.SetString(contents)
		secret.injectValuePtr(reinterpretAs(field, secret.GetType()).Addr().Interface())
		entry.appliedPath = path
		entry.appliedValue = contents
	}
	return nil
}

func readSecretFile(path string) (string, error) {
	if err := validateFile(path); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("file %q is not a regular file", path)
	}
	contents, err := io.ReadAll(file)
	return string(contents), err
}
