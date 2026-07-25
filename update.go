package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const githubReleaseURL = "https://api.github.com/repos/DistroByte/photo-organiser/releases/latest"

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func currentVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}

func runUpdate(cmd *cobra.Command, args []string) {
	current := currentVersion()

	rel, err := fetchLatestRelease(githubReleaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to fetch latest release")
	}

	if current != "" && current == rel.TagName {
		fmt.Printf("already up to date (%s)\n", current)
		return
	}

	suffix := runtime.GOOS + "-" + runtime.GOARCH
	assetName := "photo-organiser-" + suffix

	var downloadURL string
	for _, asset := range rel.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		log.Fatal().Str("asset", assetName).Msg("no release asset found for this platform")
	}

	if current != "" {
		fmt.Printf("updating %s -> %s\n", current, rel.TagName)
	} else {
		fmt.Printf("installing %s\n", rel.TagName)
	}

	if err := downloadAndReplace(downloadURL); err != nil {
		log.Fatal().Err(err).Msg("update failed")
	}

	fmt.Println("update complete")
}

func fetchLatestRelease(url string) (*githubRelease, error) {
	resp, err := httpClient.Get(url) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func downloadAndReplace(url string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("could not resolve executable path: %w", err)
	}

	resp, err := httpClient.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(exePath), ".photo-organiser-update-*")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	_, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("could not write update: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("could not flush update: %w", closeErr)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("could not make binary executable: %w", err)
	}

	if err := os.Rename(tmpPath, exePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("could not replace binary: %w", err)
	}

	return nil
}
