package main

import (
	"bytes"
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

const uploadAttempts = 4

// uploadRetryBase is a variable so tests can shrink the backoff.
var uploadRetryBase = 3 * time.Second

func uploadByDate(groups []dateGroup) {
	if len(groups) == 0 {
		log.Warn().Msg("no files to upload")
		return
	}
	cache := loadCache(defaultCachePath())
	start := time.Now()
	for _, group := range groups {
		log.Info().Str("date", group.date).Str("source", group.sourceDir).Msg("uploading")
		err := uploadGroup(group, cache)
		cache.flush()
		if err != nil {
			log.Fatal().Err(err).Str("date", group.date).Msg("upload failed")
		}
	}
	log.Info().Str("elapsed", time.Since(start).Round(time.Millisecond).String()).Msg("upload completed")
}

func uploadGroup(group dateGroup, cache *uploadCache) error {
	files := group.files
	if files == nil {
		var err error
		if files, err = uploadableFiles(group.sourceDir); err != nil {
			return err
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

// uploadableFiles lists the names of files in dir that are worth sending to
// Immich: everything except subdirectories and filesystem artefacts.
func uploadableFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if isSystemFile(entry.Name()) {
			log.Debug().Str("file", entry.Name()).Msg("skipping system file")
			continue
		}
		files = append(files, entry.Name())
	}
	return files, nil
}

func uploadFile(path string, cache *uploadCache) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	name := filepath.Base(path)
	key := cacheKey(name, info.Size())
	if id, ok := cache.has(key); ok {
		log.Debug().Str("file", name).Str("id", id).Msg("skipped (cached)")
		return nil
	}

	if dryRun {
		log.Info().Str("file", name).Msg("[dry-run] would upload")
		return nil
	}

	// fileCreatedAt should reflect when the photo was taken (EXIF), falling back to
	// mtime; fileModifiedAt stays mtime. photoDate handles the EXIF/mtime fallback.
	created := info.ModTime()
	if taken, dateErr := photoDate(path); dateErr == nil {
		created = taken
	}

	body, contentType, err := assetBody(path, created, info.ModTime())
	if err != nil {
		return err
	}

	result, err := postAsset(body, contentType, name)
	if err != nil {
		return err
	}

	// Cache both new uploads and server-side duplicates so future runs skip them.
	cache.mark(key, result.ID)

	if result.Status == "duplicate" {
		log.Debug().Str("file", name).Str("id", result.ID).Msg("duplicate (cached for next run)")
	} else {
		log.Info().Str("file", name).Str("id", result.ID).Msg("uploaded")
	}
	return nil
}

// assetBody builds the whole multipart payload in memory. Buffering rather than
// streaming from an io.Pipe is what lets net/http set Content-Length: a pipe has
// unknown length, so the request goes out chunked, and nginx spools a chunked
// body to disk in full before forwarding it — slow enough on a 40MB RAW to trip
// client_body_timeout and surface as a 499. Photos are tens of MB and videos go
// via rsync, so holding one in memory is cheap. It also makes retries trivial.
func assetBody(path string, created, modified time.Time) (*bytes.Reader, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = f.Close() }()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	fw, err := mw.CreateFormFile("assetData", filepath.Base(path))
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, "", err
	}

	for _, field := range [][2]string{
		{"deviceAssetId", filepath.Base(path)},
		{"deviceId", "photo-organiser"},
		{"fileCreatedAt", created.UTC().Format(time.RFC3339)},
		{"fileModifiedAt", modified.UTC().Format(time.RFC3339)},
		{"isFavorite", "false"},
	} {
		if err := mw.WriteField(field[0], field[1]); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}

	return bytes.NewReader(buf.Bytes()), mw.FormDataContentType(), nil
}

// postAsset uploads the payload, retrying transient failures with a growing
// backoff. Large uploads trip proxy timeouts intermittently and one blip should
// not cost the file.
func postAsset(body *bytes.Reader, contentType, name string) (*immichUploadResponse, error) {
	for attempt := 1; ; attempt++ {
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}

		result, status, err := postAssetOnce(body, contentType)
		if err == nil {
			return result, nil
		}
		if attempt == uploadAttempts || !worthRetrying(status) {
			return nil, err
		}

		delay := uploadRetryBase << (attempt - 1)
		log.Warn().Err(err).Str("file", name).Str("retry_in", delay.String()).Msg("upload failed, retrying")
		time.Sleep(delay)
	}
}

// postAssetOnce performs a single upload attempt. The returned status is 0 when
// the request failed before any response arrived.
func postAssetOnce(body *bytes.Reader, contentType string) (*immichUploadResponse, int, error) {
	req, err := http.NewRequest(http.MethodPost, immichServer+"/assets", body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-api-key", immichKey)

	resp, err := uploadClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, resp.StatusCode, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, payload)
	}

	var result immichUploadResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("parsing response: %w", err)
	}
	return &result, resp.StatusCode, nil
}

// statusClientClosedRequest is nginx's non-standard 499. nginx logs it when a
// client disconnects mid-request and never sends it downstream itself, so
// receiving one means an outer proxy relayed it after an inner one gave up on a
// slow upload. That is a timeout rather than a rejection, so it is retryable.
const statusClientClosedRequest = 499

// worthRetrying reports whether a failed attempt might succeed if repeated.
// A status of 0 means no response arrived at all: a dial failure, reset, or
// header timeout, all of which are transient.
func worthRetrying(status int) bool {
	switch status {
	case 0, http.StatusRequestTimeout, http.StatusTooManyRequests, statusClientClosedRequest:
		return true
	}
	return status >= http.StatusInternalServerError
}
