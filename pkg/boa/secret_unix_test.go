//go:build unix

package boa

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestSecretFor_RejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	type Params struct {
		Token     string `secret:"true"`
		TokenFile string `secretfor:"Token"`
	}
	done := make(chan error, 1)
	go func() {
		done <- (Cmd[Params]{Use: "test", RunFunc: func(*Params, *cobra.Command, []string) {}}).
			RunArgsE([]string{"--token-file", path})
	}()
	select {
	case err := <-done:
		if err == nil || !IsUserInputError(err) || !strings.Contains(err.Error(), "regular file") || !strings.Contains(err.Error(), path) {
			t.Fatalf("expected identifying user-input error for FIFO, got %v", err)
		}
	case <-time.After(time.Second):
		// Release a blocked reader so a regression does not leave a goroutine behind.
		writer, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("command did not finish after opening the FIFO writer")
		}
		t.Fatal("command blocked on FIFO instead of rejecting it")
	}
}
