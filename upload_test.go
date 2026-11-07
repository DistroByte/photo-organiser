package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// serveUploads stands up a fake Immich, points the package's upload settings at
// it, and records every request the client makes.
func serveUploads(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *[]*http.Request {
	t.Helper()

	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	origServer, origKey, origBackoff := immichServer, immichKey, uploadRetryBase
	immichServer, immichKey, uploadRetryBase = srv.URL, "test-key", time.Millisecond
	t.Cleanup(func() {
		immichServer, immichKey, uploadRetryBase = origServer, origKey, origBackoff
	})

	return &seen
}

// TestUploadSendsContentLength guards the fix for the 499s: an io.Pipe body has
// unknown length, so the request goes out chunked and nginx buffers the whole
// thing to disk before forwarding. A declared Content-Length avoids that.
func TestUploadSendsContentLength(t *testing.T) {
	requests := serveUploads(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"abc","status":"created"}`))
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "A6706194.ARW")
	writeFile(t, path, "raw sensor data", time.Time{})

	cache := &uploadCache{entries: map[string]string{}}
	if err := uploadFile(path, cache); err != nil {
		t.Fatal(err)
	}

	if len(*requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(*requests))
	}
	req := (*requests)[0]
	if req.ContentLength <= 0 {
		t.Errorf("ContentLength = %d, want a positive value (body would be chunked)", req.ContentLength)
	}
	if len(req.TransferEncoding) != 0 {
		t.Errorf("TransferEncoding = %v, want none", req.TransferEncoding)
	}
	if got := cache.entries[cacheKey("A6706194.ARW", 15)]; got != "abc" {
		t.Errorf("cached id = %q, want abc", got)
	}
}

func TestUploadRetriesTransientFailures(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		wantRequests int
	}{
		{"nginx client closed request", 499, 3},
		{"bad gateway", http.StatusBadGateway, 3},
		{"too many requests", http.StatusTooManyRequests, 3},
		{"payload too large is fatal", http.StatusRequestEntityTooLarge, 1},
		{"unauthorised is fatal", http.StatusUnauthorized, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			requests := serveUploads(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				// Fail twice, then succeed, so a retrying client recovers.
				if calls < 3 {
					w.WriteHeader(tt.status)
					return
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":"abc","status":"created"}`))
			})

			path := filepath.Join(t.TempDir(), "A6706194.ARW")
			writeFile(t, path, "raw", time.Time{})

			cache := &uploadCache{entries: map[string]string{}}
			err := uploadFile(path, cache)

			if tt.wantRequests == 1 && err == nil {
				t.Error("expected a permanent failure to be returned")
			}
			if tt.wantRequests > 1 && err != nil {
				t.Errorf("expected recovery after retries, got %v", err)
			}
			if len(*requests) != tt.wantRequests {
				t.Errorf("got %d requests, want %d", len(*requests), tt.wantRequests)
			}
		})
	}
}

func TestUploadGivesUpAfterMaxAttempts(t *testing.T) {
	requests := serveUploads(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(499)
	})

	path := filepath.Join(t.TempDir(), "A6706194.ARW")
	writeFile(t, path, "raw", time.Time{})

	cache := &uploadCache{entries: map[string]string{}}
	if err := uploadFile(path, cache); err == nil {
		t.Fatal("expected an error once attempts are exhausted")
	}
	if len(*requests) != uploadAttempts {
		t.Errorf("got %d requests, want %d", len(*requests), uploadAttempts)
	}
	if len(cache.entries) != 0 {
		t.Errorf("a failed upload must not be cached, got %v", cache.entries)
	}
}

func TestWorthRetrying(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{0, true},   // transport failure, no response
		{499, true}, // nginx client closed request
		{http.StatusRequestTimeout, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusGatewayTimeout, true},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusRequestEntityTooLarge, false},
		{http.StatusOK, false},
	}
	for _, tt := range tests {
		if got := worthRetrying(tt.status); got != tt.want {
			t.Errorf("worthRetrying(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

// TestAssetBodyRoundTrips checks the payload the server actually receives.
func TestAssetBodyRoundTrips(t *testing.T) {
	var gotFilename, gotDeviceID, gotCreated string
	serveUploads(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parsing multipart body: %v", err)
		}
		if files := r.MultipartForm.File["assetData"]; len(files) == 1 {
			gotFilename = files[0].Filename
		}
		gotDeviceID = r.FormValue("deviceId")
		gotCreated = r.FormValue("fileCreatedAt")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"abc","status":"created"}`))
	})

	path := filepath.Join(t.TempDir(), "A6706194.ARW")
	writeFile(t, path, "raw sensor data", noonUTC(2026, time.September, 28))

	if err := uploadFile(path, &uploadCache{entries: map[string]string{}}); err != nil {
		t.Fatal(err)
	}

	if gotFilename != "A6706194.ARW" {
		t.Errorf("filename = %q, want A6706194.ARW", gotFilename)
	}
	if gotDeviceID != "photo-organiser" {
		t.Errorf("deviceId = %q, want photo-organiser", gotDeviceID)
	}
	// No EXIF in the fixture, so fileCreatedAt falls back to mtime.
	if gotCreated != "2026-09-28T12:00:00Z" {
		t.Errorf("fileCreatedAt = %q, want 2026-09-28T12:00:00Z", gotCreated)
	}
}
