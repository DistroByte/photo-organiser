package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr string
	}{
		{
			name: "quoted values with comments and blank lines",
			input: `# header comment

server = "https://immich.local/api"   # trailing comment
key = "abc\"def"
user = ""
`,
			want: map[string]string{"server": "https://immich.local/api", "key": `abc"def`, "user": ""},
		},
		{
			name:  "literal string keeps backslashes",
			input: `remote_path = 'C:\photos'`,
			want:  map[string]string{"remote_path": `C:\photos`},
		},
		{
			name:  "bare value is accepted for hand edits",
			input: "device = /dev/sdd1 # the card reader\nmount_type = exfat",
			want:  map[string]string{"device": "/dev/sdd1", "mount_type": "exfat"},
		},
		{
			name:    "table header is rejected",
			input:   "[immich]\nserver = \"x\"",
			wantErr: "line 1: tables are not supported",
		},
		{
			name:    "line without equals",
			input:   "server",
			wantErr: "line 1: expected key = value",
		},
		{
			name:    "unterminated string",
			input:   `server = "https://x`,
			wantErr: "line 1: unterminated string",
		},
		{
			name:    "garbage after quoted value",
			input:   `server = "x" y`,
			wantErr: "line 1: unexpected text after value: y",
		},
		{
			name:    "duplicate key",
			input:   "key = \"a\"\nkey = \"b\"",
			wantErr: "line 2: duplicate key",
		},
		{
			name:    "missing value",
			input:   "key =",
			wantErr: "line 1: missing value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConfig(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("%s = %q, want %q", key, got[key], want)
				}
			}
		})
	}
}

func TestWriteConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	values := map[string]string{
		"server":      "https://immich.local/api",
		"key":         `se"cret\`,
		"remote_path": "/volume1/photos",
	}

	if err := writeConfig(path, values); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600", perm)
	}

	got, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range values {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}
	// Every known key is written so the user can see what exists.
	for _, entry := range configEntries {
		if _, ok := got[entry.key]; !ok {
			t.Errorf("key %s missing from written config", entry.key)
		}
	}

	// Rewriting replaces the content and tightens permissions on a file that
	// was loosened by hand.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	values["server"] = "https://other/api"
	if err := writeConfig(path, values); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("rewrite left mode %o, want 600", perm)
	}
	got, _ = readConfig(path)
	if got["server"] != "https://other/api" {
		t.Errorf("rewrite did not replace server, got %q", got["server"])
	}
}

func TestReadConfigMissingFile(t *testing.T) {
	got, err := readConfig(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("missing config should not be an error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// saveGlobals restores the flag-bound package variables after a test that
// builds a root command, since registering the flags resets them.
func saveGlobals(t *testing.T) {
	t.Helper()
	origServer, origKey, origUser, origDevice, origHost := immichServer, immichKey, remoteUser, device, remoteHost
	t.Cleanup(func() {
		immichServer, immichKey, remoteUser, device, remoteHost = origServer, origKey, origUser, origDevice, origHost
	})
}

func TestApplyConfigPrecedence(t *testing.T) {
	saveGlobals(t)

	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, `server = "https://file/api"
key = "file-key"
user = "file-user"
host = "nas"
`, noonUTC(2026, 1, 1))
	t.Setenv("IMMICH_SERVER", "https://env/api")
	t.Setenv("IMMICH_API_KEY", "")

	cmd := newRootCmd()
	if err := cmd.ParseFlags([]string{"--host", "flag-host"}); err != nil {
		t.Fatal(err)
	}
	if err := applyConfig(cmd.Flags(), path); err != nil {
		t.Fatal(err)
	}

	checks := map[string][2]string{
		"flag beats file":       {remoteHost, "flag-host"},
		"env beats file":        {immichServer, "https://env/api"},
		"file fills unset flag": {immichKey, "file-key"},
		"file beats $USER":      {remoteUser, "file-user"},
		"default survives":      {device, "/dev/sdd1"},
	}
	for name, check := range checks {
		if check[0] != check[1] {
			t.Errorf("%s: got %q, want %q", name, check[0], check[1])
		}
	}
}

func TestApplyConfigRejectsBrokenFile(t *testing.T) {
	saveGlobals(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, "[broken", noonUTC(2026, 1, 1))

	cmd := newRootCmd()
	if err := applyConfig(cmd.Flags(), path); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestPromptConfig(t *testing.T) {
	values := map[string]string{"server": "https://old/api", "key": "oldkey1234", "host": "nas"}
	// First answer replaces server, second keeps the key, then input ends so
	// every remaining setting keeps its default.
	in := strings.NewReader("https://new/api\n\n")
	var out strings.Builder

	if err := promptConfig(in, &out, values); err != nil {
		t.Fatal(err)
	}

	if values["server"] != "https://new/api" {
		t.Errorf("server = %q, want https://new/api", values["server"])
	}
	if values["key"] != "oldkey1234" {
		t.Errorf("key = %q, want unchanged", values["key"])
	}
	if values["host"] != "nas" {
		t.Errorf("host = %q, want unchanged", values["host"])
	}
	if strings.Contains(out.String(), "oldkey1234") {
		t.Error("prompt echoed the full API key")
	}
	if !strings.Contains(out.String(), "[****1234]") {
		t.Errorf("prompt should show the masked key, got:\n%s", out.String())
	}
}

func TestMaskSecret(t *testing.T) {
	tests := map[string]string{"": "", "abc": "****", "abcd": "****", "abcdefgh": "****efgh"}
	for in, want := range tests {
		if got := maskSecret(in); got != want {
			t.Errorf("maskSecret(%q) = %q, want %q", in, got, want)
		}
	}
}
