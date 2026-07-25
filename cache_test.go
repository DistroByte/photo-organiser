package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheKey(t *testing.T) {
	tests := []struct {
		name string
		size int64
		want string
	}{
		{"DSC0001.JPG", 1024, "DSC0001.JPG:1024"},
		{"C0023.MP4", 0, "C0023.MP4:0"},
		{"file with spaces.raw", 42, "file with spaces.raw:42"},
	}
	for _, tt := range tests {
		if got := cacheKey(tt.name, tt.size); got != tt.want {
			t.Errorf("cacheKey(%q, %d) = %q, want %q", tt.name, tt.size, got, tt.want)
		}
	}
}

func TestUploadCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "uploaded.json")

	cache := loadCache(path)
	if _, ok := cache.has("a:1"); ok {
		t.Fatal("fresh cache should not contain any key")
	}

	cache.mark("a:1", "asset-a")
	cache.mark("b:2", "asset-b")
	cache.flush()

	reloaded := loadCache(path)
	if id, ok := reloaded.has("a:1"); !ok || id != "asset-a" {
		t.Errorf("reloaded cache has(a:1) = (%q, %v), want (asset-a, true)", id, ok)
	}
	if id, ok := reloaded.has("b:2"); !ok || id != "asset-b" {
		t.Errorf("reloaded cache has(b:2) = (%q, %v), want (asset-b, true)", id, ok)
	}
}

func TestUploadCacheFlushOnlyWhenDirty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uploaded.json")
	cache := loadCache(path)

	// mark alone must not touch disk; batching is the whole point.
	cache.mark("a:1", "asset-a")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("mark should not write the cache file before flush")
	}

	cache.flush()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("flush should have written the cache file: %v", err)
	}
}

func TestLoadCacheMissingFile(t *testing.T) {
	cache := loadCache(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if cache == nil || cache.entries == nil {
		t.Fatal("loadCache should return an initialised cache for a missing file")
	}
	if len(cache.entries) != 0 {
		t.Errorf("expected empty cache, got %d entries", len(cache.entries))
	}
}

func TestLoadCacheCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uploaded.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	cache := loadCache(path)
	if len(cache.entries) != 0 {
		t.Errorf("corrupt cache should start fresh, got %d entries", len(cache.entries))
	}
}
