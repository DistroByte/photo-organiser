package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

// Remove directories that photos have been backed up from
func cleanupSourceDirs(sourceDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dirPath := filepath.Join(sourceDir, entry.Name())
			log.Debug().Str("dir", dirPath).Msg("Removing directory during cleanup")
			if err := os.RemoveAll(dirPath); err != nil {
				log.Warn().Str("dir", dirPath).Err(err).Msg("Failed to remove directory during cleanup")
			}
		}
	}
	return nil
}

// Prompt the user before triggering the cleanup
func promptAndCleanup() {
	if dryRun {
		log.Info().Msg("Dry run complete. No files were actually moved or deleted.")
		return
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Cleanup source directories? [y/N]: ")
	input, _ := reader.ReadString('\n')
	if len(input) > 0 && (input[0] == 'y' || input[0] == 'Y') {
		if err := cleanupSourceDirs(sourceDir); err != nil {
			log.Fatal().Err(err).Msg("Failed to cleanup source directories")
		}
		log.Info().Msg("Source directories cleaned up.")
	} else {
		log.Info().Msg("Skipping cleanup of source directories.")
	}
}
