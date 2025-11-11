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
		"--rsync-path=/bin/rsync",
		"--exclude", "CANONMSC",
		"--exclude", "100CANON",
		"--ignore-existing",
		"--info=none,progress2",
		shortFlags,
		rsyncSource,
		fmt.Sprintf("%s@%s:%s", remoteUser, remoteHost, remotePath),
	}

	log.Info().Str("source", rsArgs[len(rsArgs)-2]).Str("dest", rsArgs[len(rsArgs)-1]).Msg("Starting rsync from source to destination")
	rsCmd := exec.Command("rsync", rsArgs...)
	rsCmd.Stdout = os.Stdout
	rsCmd.Stderr = os.Stderr
	if err := rsCmd.Run(); err != nil {
		log.Fatal().Err(err).Msg("rsync failed")
	}
	log.Info().Msg("rsync completed successfully.")
}
