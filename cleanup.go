package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

func cleanupSourceDirs(sourceDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dirPath := filepath.Join(sourceDir, entry.Name())
			log.Debug().Str("dir", dirPath).Msg("removing directory during cleanup")
			if err := os.RemoveAll(dirPath); err != nil {
				log.Warn().Str("dir", dirPath).Err(err).Msg("failed to remove directory during cleanup")
			}
		}
	}
	return nil
}

func cleanupFlatSourceFiles(sourceDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(sourceDir, entry.Name())
		log.Debug().Str("file", path).Msg("removing file during cleanup")
		if err := os.Remove(path); err != nil {
			log.Warn().Str("file", path).Err(err).Msg("failed to remove file during cleanup")
		}
	}
	return nil
}

func promptAndCleanup() bool {
	if dryRun {
		log.Info().Msg("Dry run complete. No files were actually moved or deleted.")
		return false
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Cleanup source directories? [y/N]: ")
	input, _ := reader.ReadString('\n')
	if len(input) > 0 && (input[0] == 'y' || input[0] == 'Y') {
		if err := cleanupSourceDirs(sourceDir); err != nil {
			log.Fatal().Err(err).Msg("failed to cleanup source directories")
		}
		log.Info().Msg("Source directories cleaned up.")
		return true
	}
	log.Info().Msg("Skipping cleanup of source directories.")
	return false
}

// cleanupSonyCardIndex removes the Sony card ownership index and the auto-image
// index directory so the camera does not complain about missing files on reinsertion.
func cleanupSonyCardIndex(cardRoot string) {
	targets := []string{
		filepath.Join(cardRoot, "PRIVATE", "SONY", "SONYCARD.IND"),
		filepath.Join(cardRoot, "AVF_INFO"),
	}
	for _, target := range targets {
		log.Debug().Str("path", target).Msg("removing Sony card index entry")
		if err := os.RemoveAll(target); err != nil {
			log.Warn().Str("path", target).Err(err).Msg("failed to remove Sony card index entry")
		}
	}
	log.Info().Msg("Sony card index cleared.")
}

func promptAndCleanupFlat() bool {
	if dryRun {
		log.Info().Msg("Dry run complete. No files were actually moved or deleted.")
		return false
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Cleanup source files? [y/N]: ")
	input, _ := reader.ReadString('\n')
	if len(input) > 0 && (input[0] == 'y' || input[0] == 'Y') {
		if err := cleanupFlatSourceFiles(sourceDir); err != nil {
			log.Fatal().Err(err).Msg("failed to cleanup source files")
		}
		log.Info().Msg("Source files cleaned up.")
		return true
	}
	log.Info().Msg("Skipping cleanup of source files.")
	return false
}
