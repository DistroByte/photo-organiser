package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	tests := map[string]string{
		"~/bin/immich-stack":   filepath.Join(home, "bin", "immich-stack"),
		"~":                    home,
		"/usr/local/bin/stack": "/usr/local/bin/stack",
		"immich-stack":         "immich-stack",
		"~user/bin":            "~user/bin",
	}
	for in, want := range tests {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStackCommand(t *testing.T) {
	saveGlobals(t)
	immichServer, immichKey = "https://immich.test/api", "sk-test"

	binary := filepath.Join(t.TempDir(), "immich-stack")
	writeFile(t, binary, "#!/bin/sh\n", time.Time{})
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}

	stack, err := stackCommand(binary, []string{"fix-trash", "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	if stack.Path != binary {
		t.Errorf("Path = %s, want %s", stack.Path, binary)
	}
	if got := strings.Join(stack.Args[1:], " "); got != "fix-trash --dry-run" {
		t.Errorf("args = %q, want them passed through unchanged", got)
	}
	var sawURL, sawKey bool
	for _, kv := range stack.Env {
		sawURL = sawURL || kv == "API_URL=https://immich.test/api"
		sawKey = sawKey || kv == "API_KEY=sk-test"
	}
	if !sawURL || !sawKey {
		t.Errorf("env missing API_URL/API_KEY: url=%v key=%v", sawURL, sawKey)
	}
	for _, arg := range stack.Args {
		if strings.Contains(arg, "sk-test") {
			t.Error("API key must not appear in argv")
		}
	}
}

func TestStackCommandMissingBinary(t *testing.T) {
	if _, err := stackCommand(filepath.Join(t.TempDir(), "nope"), nil); err == nil {
		t.Fatal("expected an error for a missing binary")
	}
}
