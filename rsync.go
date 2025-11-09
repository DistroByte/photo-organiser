package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/rs/zerolog/log"
)

// rsyncToRemote runs rsync to copy files from the source directory to the remote destination.
func rsyncToRemote() {
	shortFlags := "-avhPz"
	if dryRun {
		shortFlags = "-avhPzn"
	}
	// Ensure sourceDir ends with a trailing slash for rsync to copy contents, not the directory itself
	rsyncSource := sourceDir
	if !strings.HasSuffix(rsyncSource, string(os.PathSeparator)) {
		rsyncSource += string(os.PathSeparator)
	}
	rsArgs := []string{
		shortFlags,
		"--ignore-existing",
		"--rsync-path=/bin/rsync",
		"--exclude", "CANONMSC",
		"--exclude", "100CANON",
		rsyncSource,
		fmt.Sprintf("%s@%s:%s", remoteUser, remoteHost, remotePath),
	}

	log.Info().Strs("args", rsArgs).Msg("Starting rsync to remote destination...")
	rsCmd := exec.Command("rsync", rsArgs...)
	rsCmd.Stdout = os.Stdout
	rsCmd.Stderr = os.Stderr
	if err := rsCmd.Run(); err != nil {
		log.Fatal().Err(err).Msg("rsync failed")
	}
	log.Info().Msg("rsync completed successfully.")
}
