package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelativeConfigAndInput(t *testing.T) {
	base, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := command(&out).RunArgsE([]string{"--data-dir", base}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Config: "+filepath.Join(base, "config.json")) || !strings.Contains(out.String(), "Input: "+filepath.Join(base, "input.txt")) || !strings.Contains(out.String(), "State: "+filepath.Join(base, "state.db")) {
		t.Fatalf("output: %s", &out)
	}
}

func TestCreateEmptyBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "new", "data")
	if err := command(&bytes.Buffer{}).RunArgsE([]string{"--data-dir", base}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		t.Fatalf("directory: %v, %v", info, err)
	}
}

func TestNetworkConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("APP_DATA_DIR", "")
	t.Setenv("APP_NETWORK", "")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"Network":"testnet"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := command(&out).RunArgsE(nil); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "data", "testnet")
	if !strings.Contains(out.String(), "Data directory: "+base) || !strings.Contains(out.String(), "State: "+filepath.Join(base, "state.db")) {
		t.Fatalf("output: %s", &out)
	}
}
