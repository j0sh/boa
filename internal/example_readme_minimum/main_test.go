package main

import (
	"os"
	"testing"
)

func TestMain(t *testing.T) {
	prevArgs := os.Args
	defer func() { os.Args = prevArgs }()

	os.Args = []string{"greet", "--name", "Ada"}
	main()
}

func TestMainWithAllArgs(t *testing.T) {
	prevArgs := os.Args
	defer func() { os.Args = prevArgs }()

	os.Args = []string{"greet", "--name", "Ada", "--count", "2", "--excited"}
	main()
}
