package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mkdir(t *testing.T, path string) {
	t.Helper()
	writeFile(t, filepath.Join(path, ".keep"), "", time.Time{})
}

func TestDetectLayout(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(root string)
		want    string
		wantErr string
	}{
		{
			name:  "sony by PRIVATE/SONY",
			setup: func(root string) { mkdir(t, filepath.Join(root, "PRIVATE", "SONY")) },
			want:  "sony",
		},
		{
			name:  "sony by AVF_INFO alone",
			setup: func(root string) { mkdir(t, filepath.Join(root, "AVF_INFO")) },
			want:  "sony",
		},
		{
			name:  "dji",
			setup: func(root string) { mkdir(t, filepath.Join(root, "DCIM", "DJI_001")) },
			want:  "dji",
		},
		{
			name:  "canon by numbered folder",
			setup: func(root string) { mkdir(t, filepath.Join(root, "DCIM", "100CANON")) },
			want:  "canon",
		},
		{
			name:  "canon by CANONMSC",
			setup: func(root string) { mkdir(t, filepath.Join(root, "DCIM", "CANONMSC")) },
			want:  "canon",
		},
		{
			name:  "charmera has loose media and no DCIM",
			setup: func(root string) { writeFile(t, filepath.Join(root, "IMG001.JPG"), "p", time.Time{}) },
			want:  "charmera",
		},
		{
			name: "AppleDouble stubs do not make a charmera",
			setup: func(root string) {
				writeFile(t, filepath.Join(root, "._IMG001.JPG"), "x", time.Time{})
			},
			wantErr: "could not recognise",
		},
		{
			name:    "empty card",
			setup:   func(root string) {},
			wantErr: "could not recognise",
		},
		{
			name: "two cameras' markers is ambiguous",
			setup: func(root string) {
				mkdir(t, filepath.Join(root, "PRIVATE", "SONY"))
				mkdir(t, filepath.Join(root, "DCIM", "DJI_001"))
			},
			wantErr: "looks like sony and dji",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.setup(root)
			layout, err := detectLayout(root)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if layout.name != tt.want {
				t.Errorf("detected %s, want %s", layout.name, tt.want)
			}
		})
	}
}

func TestPickLayout(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "DCIM", "DJI_001"))

	// An override wins even when the card says otherwise.
	layout, err := pickLayout(root, "canon")
	if err != nil || layout.name != "canon" {
		t.Errorf("pickLayout(canon) = (%s, %v), want canon", layout.name, err)
	}
	if _, err := pickLayout(root, "nikon"); err == nil || !strings.Contains(err.Error(), "unknown camera") {
		t.Errorf("unknown override: err = %v", err)
	}
	layout, err = pickLayout(root, "")
	if err != nil || layout.name != "dji" {
		t.Errorf("pickLayout(detect) = (%s, %v), want dji", layout.name, err)
	}
}

func TestResolveSources(t *testing.T) {
	sony, _ := layoutByName("sony")

	t.Run("absent sources are dropped", func(t *testing.T) {
		root := t.TempDir()
		mkdir(t, filepath.Join(root, "DCIM"))

		active := resolveSources(sony, root, "")
		if len(active) != 1 || active[0].name != "sony photos" {
			t.Fatalf("got %+v, want only sony photos", active)
		}
		if active[0].path != filepath.Join(root, "DCIM") {
			t.Errorf("path = %s, want %s", active[0].path, filepath.Join(root, "DCIM"))
		}
	})

	t.Run("every present source is included", func(t *testing.T) {
		root := t.TempDir()
		mkdir(t, filepath.Join(root, "DCIM"))
		mkdir(t, filepath.Join(root, "PRIVATE", "M4ROOT", "CLIP"))

		active := resolveSources(sony, root, "")
		if len(active) != 2 {
			t.Fatalf("got %d sources, want 2", len(active))
		}
		if !active[1].rsyncOnly {
			t.Error("sony videos should keep rsyncOnly")
		}
	})

	t.Run("override replaces the primary source and drops the rest", func(t *testing.T) {
		active := resolveSources(sony, t.TempDir(), "/elsewhere")
		if len(active) != 1 || active[0].path != "/elsewhere" || active[0].name != "sony photos" {
			t.Fatalf("got %+v, want sony photos at /elsewhere", active)
		}
	})

	t.Run("override matching a source folder picks that source", func(t *testing.T) {
		active := resolveSources(sony, t.TempDir(), "/mnt/card/PRIVATE/M4ROOT/CLIP")
		if len(active) != 1 || active[0].name != "sony videos" || !active[0].rsyncOnly {
			t.Fatalf("got %+v, want sony videos", active)
		}
	})
}

func TestLayoutNames(t *testing.T) {
	if got := layoutNames(); got != "sony, dji, canon, charmera" {
		t.Errorf("layoutNames() = %q", got)
	}
}
