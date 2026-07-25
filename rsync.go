package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

var rsyncBin = "rsync"

func rsyncByDate(groups []dateGroup) {
	if len(groups) == 0 {
		log.Warn().Msg("no files to sync")
		return
	}
	start := time.Now()
	for _, group := range groups {
		log.Info().Str("date", group.date).Str("source", group.sourceDir).Msg("syncing")
		if err := runRsync(group); err != nil {
			log.Fatal().Err(err).Str("date", group.date).Msg("rsync failed")
		}
	}
	log.Info().Str("elapsed", time.Since(start).Round(time.Millisecond).String()).Msg("rsync completed")
}

func runRsync(group dateGroup) error {
	dest := fmt.Sprintf("%s@%s:%s/%s", remoteUser, remoteHost, remotePath, group.date)
	source := group.sourceDir
	if !strings.HasSuffix(source, string(os.PathSeparator)) {
		source += string(os.PathSeparator)
	}

	args := baseRsyncArgs()

	if group.files != nil {
		tmp, err := os.CreateTemp("", "photo-organiser-*.txt")
		if err != nil {
			return fmt.Errorf("creating file list: %w", err)
		}
		defer func() { _ = os.Remove(tmp.Name()) }()

		if _, err := fmt.Fprintln(tmp, strings.Join(group.files, "\n")); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("writing file list: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("closing file list: %w", err)
		}
		args = append(args, "--files-from="+tmp.Name())
	}

	args = append(args, source, dest)
	cmd := exec.Command(rsyncBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func baseRsyncArgs() []string {
	flags := []string{"--archive", "--verbose", "--human-readable"}
	if dryRun {
		flags = append(flags, "--dry-run")
	}
	return append([]string{
		"--rsync-path=/bin/rsync",
		"--ignore-existing",
		"--info=none,progress2",
		"--chmod=ugo=rwX",
	}, flags...)
}
