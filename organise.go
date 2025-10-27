package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/rwcarlsen/goexif/exif"
)

var sonyFolderNameRegex = regexp.MustCompile(`^\d{8}$`)
var djiFilenameRegex = regexp.MustCompile(`^DJI_(\d{4})(\d{2})(\d{2})\d{6}_\d+_\w\..+$`)
var isoDateRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func organiseSonyPhotos(sourceDir string, dryRun bool) error {
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
		if !sonyFolderNameRegex.MatchString(parentDir) {
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

func organiseDJIPhotos(sourceDir string, dryRun bool) error {
	log.Info().Str("source", sourceDir).Bool("dry-run", dryRun).Msg("Starting DJI action photo organisation")
	return filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
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
}

func organiseCanonPhotos(sourceDir string, dryRun bool) error {
	log.Info().Str("source", sourceDir).Bool("dry-run", dryRun).Msg("Starting Canon photo organisation")
	return filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(sourceDir, path)
		if relErr != nil {
			return relErr
		}
		baseRel := filepath.Base(rel)

		// skip descending into date destination directories
		if d.IsDir() && isoDateRegex.MatchString(baseRel) {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}

		parentDir := filepath.Base(filepath.Dir(rel))
		if isoDateRegex.MatchString(parentDir) {
			log.Debug().Str("file", path).Str("parent", parentDir).Msg("Skipping file already in date folder")
			return nil
		}

		// Skip CANONMSC directories (case-insensitive)
		relSlash := filepath.ToSlash(rel)
		parts := strings.Split(relSlash, "/")
		for _, p := range parts {
			if strings.EqualFold(p, "CANONMSC") {
				log.Debug().Str("file", path).Msg("Skipping file in CANONMSC directory")
				return nil
			}
		}

		// Try EXIF date
		var taken time.Time
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		x, exifErr := exif.Decode(f)
		if exifErr == nil {
			if dt, dtErr := x.DateTime(); dtErr == nil {
				taken = dt
			}
		}

		// Fallback to file mod time
		if taken.IsZero() {
			info, statErr := os.Stat(path)
			if statErr != nil {
				return statErr
			}
			taken = info.ModTime()
		}

		destDir := filepath.Join(sourceDir, taken.Format("2006-01-02"))
		destPath := filepath.Join(destDir, filepath.Base(path))

		log.Debug().Str("source", path).Str("destination", destPath).Bool("dry-run", dryRun).Msg("Processing file")
		if dryRun {
			log.Info().Str("source", path).Str("destination", destPath).Bool("dry-run", true).Msg("Would move file")
			return nil
		}

		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return err
		}
		if err := os.Rename(path, destPath); err != nil {
			return fmt.Errorf("failed to move file %s to %s: %w", path, destPath, err)
		}
		log.Info().Str("source", path).Str("destination", destPath).Msg("File moved successfully")
		return nil
	})
}

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
