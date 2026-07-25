package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

type uploadCache struct {
	entries map[string]string // cacheKey → immich asset ID
	path    string
	dirty   bool // entries changed since the last flush
}

func defaultCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "photo-organiser", "uploaded.json")
}

func loadCache(path string) *uploadCache {
	c := &uploadCache{
		entries: make(map[string]string),
		path:    path,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", path).Msg("could not read upload cache")
		}
		return c
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		log.Warn().Err(err).Msg("could not parse upload cache, starting fresh")
		c.entries = make(map[string]string)
		return c
	}
	log.Debug().Int("entries", len(c.entries)).Str("path", path).Msg("loaded upload cache")
	return c
}

func (c *uploadCache) has(key string) (string, bool) {
	id, ok := c.entries[key]
	return id, ok
}

// mark records key→assetID in memory. Call flush to persist; batching the writes
// avoids rewriting the whole cache file once per uploaded file.
func (c *uploadCache) mark(key, assetID string) {
	c.entries[key] = assetID
	c.dirty = true
}

// flush persists the cache to disk if it has unsaved changes. It is called after
// each date group so an interrupted run keeps the progress of completed groups.
func (c *uploadCache) flush() {
	if !c.dirty {
		return
	}
	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		log.Warn().Err(err).Msg("could not marshal upload cache")
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		log.Warn().Err(err).Msg("could not create cache directory")
		return
	}
	if err := os.WriteFile(c.path, data, 0644); err != nil {
		log.Warn().Err(err).Msg("could not save upload cache")
		return
	}
	c.dirty = false
}

// cacheKey identifies a source file by name and byte size.
// Size is included as a cheap guard against two cameras producing the same filename
// with different content (different photo, same sequential number).
func cacheKey(name string, size int64) string {
	return fmt.Sprintf("%s:%d", name, size)
}
