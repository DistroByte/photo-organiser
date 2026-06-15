package main

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

type immichUploadResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func uploadByDate(groups []dateGroup) {
	if len(groups) == 0 {
		log.Warn().Msg("no files to upload")
		return
	}
	cache := loadCache(defaultCachePath())
	start := time.Now()
	for _, group := range groups {
		log.Info().Str("date", group.date).Str("source", group.sourceDir).Msg("uploading")
		if err := uploadGroup(group, cache); err != nil {
			log.Fatal().Err(err).Str("date", group.date).Msg("upload failed")
		}
	}
	log.Info().Str("elapsed", time.Since(start).Round(time.Millisecond).String()).Msg("upload completed")
}

func uploadGroup(group dateGroup, cache *uploadCache) error {
	files := group.files
	if files == nil {
		entries, err := os.ReadDir(group.sourceDir)
		if err != nil {
			return fmt.Errorf("reading directory: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, e.Name())
			}
		}
	}

	var failed int
	for _, rel := range files {
		path := filepath.Join(group.sourceDir, rel)
		if err := uploadFile(path, cache); err != nil {
			log.Error().Err(err).Str("file", rel).Msg("upload failed")
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d file(s) failed to upload", failed)
	}
	return nil
}

func uploadFile(path string, cache *uploadCache) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	key := cacheKey(filepath.Base(path), info.Size())
	if id, ok := cache.has(key); ok {
		log.Debug().Str("file", filepath.Base(path)).Str("id", id).Msg("skipped (cached)")
		return nil
	}

	if dryRun {
		log.Info().Str("file", filepath.Base(path)).Msg("[dry-run] would upload")
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	go func() {
		fw, err := mw.CreateFormFile("assetData", filepath.Base(path))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err = io.Copy(fw, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		ts := info.ModTime().UTC().Format(time.RFC3339)
		for _, pair := range [][2]string{
			{"deviceAssetId", filepath.Base(path)},
			{"deviceId", "photo-organiser"},
			{"fileCreatedAt", ts},
			{"fileModifiedAt", ts},
			{"isFavorite", "false"},
		} {
			if err := mw.WriteField(pair[0], pair[1]); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		mw.Close()
		pw.Close()
	}()

	url := immichServer + "/assets"
	req, err := http.NewRequest(http.MethodPost, url, pr)
	if err != nil {
		pr.CloseWithError(err)
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("x-api-key", immichKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var result immichUploadResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	// Cache both new uploads and server-side duplicates so future runs skip them.
	cache.mark(key, result.ID)

	if result.Status == "duplicate" {
		log.Debug().Str("file", filepath.Base(path)).Str("id", result.ID).Msg("duplicate (cached for next run)")
	} else {
		log.Info().Str("file", filepath.Base(path)).Str("id", result.ID).Msg("uploaded")
	}
	return nil
}