package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/rs/zerolog/log"
)

var folderNameRegex = regexp.MustCompile(`^\d{8}$`)
var djiFilenameRegex = regexp.MustCompile(`^DJI_(\d{4})(\d{2})(\d{2})\d{6}_\d+_\w\..+$`)

// organisePhotos organises Sony camera photos into date-based folders.
func organisePhotos(sourceDir string, dryRun bool) error {
	log.Info().Str("source", sourceDir).Bool("dry-run", dryRun).Msg("Starting photo organisation")
	dirsToRemove := make(map[string]struct{})
	err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() && path == sourceDir || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		parentDir := filepath.Base(filepath.Dir(rel))
		if !folderNameRegex.MatchString(parentDir) {
			return nil
		}
		destDirName, err := calculateDestinationDir(parentDir)
		if err != nil {
			return err
		}
		destPath := filepath.Join(sourceDir, destDirName, filepath.Base(path))
		dirsToRemove[filepath.Join(sourceDir, parentDir)] = struct{}{}
		log.Debug().Str("source", path).Str("destination", destPath).Bool("dry-run", dryRun).Msg("Processing file")
		if dryRun {
			log.Info().Str("source", path).Str("destination", destPath).Bool("dry-run", true).Msg("Would move file")
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}
		if err := os.Rename(path, destPath); err != nil {
			return fmt.Errorf("failed to move file %s to %s: %w", path, destPath, err)
		}
		log.Info().Str("source", path).Str("destination", destPath).Bool("dry-run", false).Msg("File moved successfully")
		return nil
	})
	if err != nil {
		return err
	}
	log.Debug().Any("directories", dirsToRemove).Msg("Checking for empty directories to remove")
	for dir := range dirsToRemove {
		if dryRun {
			log.Info().Str("dir", dir).Bool("dry-run", true).Msg("Would remove directory if empty")
			continue
		}
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) == 0 {
			if err := os.Remove(dir); err != nil {
				log.Warn().Str("dir", dir).Err(err).Bool("dry-run", false).Msg("Failed to remove directory")
			} else {
				log.Info().Str("dir", dir).Bool("dry-run", false).Msg("Removed empty directory")
			}
		}
	}
	return nil
}

// organiseDJIPhotos organises DJI camera (action/drone) photos into date-based folders using filename parsing.
func organiseDJIPhotos(sourceDir string, dryRun bool) error {
	log.Info().Str("source", sourceDir).Bool("dry-run", dryRun).Msg("Starting DJI action photo organisation")
	err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		base := filepath.Base(path)
		matches := djiFilenameRegex.FindStringSubmatch(base)
		if matches == nil {
			log.Debug().Str("file", base).Msg("Skipping non-DJI file")
			return nil
		}
		year, month, day := matches[1], matches[2], matches[3]
		destDir := filepath.Join(sourceDir, fmt.Sprintf("%s-%s-%s", year, month, day))
		destPath := filepath.Join(destDir, base)
		log.Debug().Str("source", path).Str("destination", destPath).Bool("dry-run", dryRun).Msg("Processing DJI file")
		if dryRun {
			log.Info().Str("source", path).Str("destination", destPath).Bool("dry-run", true).Msg("Would move file")
			return nil
		}
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return err
		}
		if err := os.Rename(path, destPath); err != nil {
			return fmt.Errorf("failed to move file %s to %s: %w", path, destPath, err)
		}
		log.Info().Str("source", path).Str("destination", destPath).Bool("dry-run", false).Msg("File moved successfully")
		return nil
	})
	return err
}

// calculateDestinationDir calculates the destination directory name (YYYY-MM-DD) from a folder name.
func calculateDestinationDir(dirName string) (string, error) {
	if len(dirName) < 8 {
		return "", fmt.Errorf("directory name %s is too short to determine date", dirName)
	}
	currentYear := time.Now().Year()
	currentYearStr := fmt.Sprintf("%d", currentYear)
	year := currentYearStr[:3] + dirName[3:4]
	month := dirName[4:6]
	day := dirName[6:8]
	return fmt.Sprintf("%s-%s-%s", year, month, day), nil
}
