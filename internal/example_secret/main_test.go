package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestSecretSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "endpoint")
	if err := os.WriteFile(path, []byte("https://user:secret@example.com/api"), 0o600); err != nil {
		t.Fatal(err)
	}
	headersPath := filepath.Join(t.TempDir(), "headers")
	const headers = "Authorization: Bearer secret, X-Api-Key: key"
	if err := os.WriteFile(headersPath, []byte(headers), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"environment", "file"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("API_URL", "")
			t.Setenv("API_URL_FILE", "")
			t.Setenv("API_HEADERS", "")
			t.Setenv("API_HEADERS_FILE", "")
			var args []string
			if source == "environment" {
				t.Setenv("API_URL", "https://user:secret@example.com/api")
				t.Setenv("API_HEADERS", headers)
			} else {
				args = []string{"--endpoint-file", path, "--headers-file", headersPath}
			}
			command := endpointCommand()
			ran := false
			command.RunFunc = func(p *Params, _ *cobra.Command, _ []string) {
				ran = true
				want := Headers{"Authorization": {"Bearer secret"}, "X-Api-Key": {"key"}}
				if p.Endpoint == nil || p.Endpoint.Host != "example.com" || !reflect.DeepEqual(p.Headers, want) {
					t.Fatal("typed endpoint or custom headers were not loaded")
				}
			}
			if err := command.RunArgsE(args); err != nil {
				t.Fatal(err)
			}
			if !ran {
				t.Fatal("command did not run")
			}
		})
	}
}

func TestEndpointValidation(t *testing.T) {
	t.Setenv("API_URL", "http://example.com")
	t.Setenv("API_URL_FILE", "")
	t.Setenv("API_HEADERS", "")
	t.Setenv("API_HEADERS_FILE", "")
	if err := endpointCommand().RunArgsE(nil); err == nil {
		t.Fatal("expected HTTPS validation failure")
	}
}
