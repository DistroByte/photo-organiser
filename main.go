package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	sourceDir  string
	dryRun     bool
	verbose    bool
	remoteUser string
	remoteHost string
	remotePath string
	mountDrive string
	mountPoint string
	mountType  string
)

var folderNameRegex = regexp.MustCompile(`^\d{8}$`)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	rootCmd := &cobra.Command{
		Use:   "photo-organiser",
		Short: "Organise Sony camera photos into a directory structure based on the date they were taken.",
		Long:  `photo-organiser is a CLI tool that takes sony camera photos and organises them into a directory structure based on the date they were taken.`,
	}

	rootCmd.PersistentFlags().StringVar(&mountDrive, "mount-drive", "", "Drive to mount (e.g. f:)")
	rootCmd.PersistentFlags().StringVar(&mountPoint, "mount-point", "", "Mount point (e.g. /mnt/f)")
	rootCmd.PersistentFlags().StringVarP(&sourceDir, "source", "s", "", "Source directory containing the photos, defaults to /mount/point/DCIM")
	rootCmd.PersistentFlags().StringVar(&remoteUser, "user", os.Getenv("USER"), "Remote user for rsync, defaults to current user")
	rootCmd.PersistentFlags().StringVar(&remoteHost, "host", "", "Remote host for rsync")
	rootCmd.PersistentFlags().StringVar(&remotePath, "remote-path", "", "Remote destination path for rsync")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "If set, will not move files but print what it would do")
	rootCmd.PersistentFlags().StringVar(&mountType, "mount-type", "drvfs", "Filesystem type for mounting (drvfs for WSL, vfat/exfat for Linux, leave empty to skip mounting)")
	rootCmd.PersistentFlags().SortFlags = false
	rootCmd.MarkPersistentFlagRequired("mount-drive")
	rootCmd.MarkPersistentFlagRequired("mount-point")
	rootCmd.MarkPersistentFlagRequired("host")
	rootCmd.MarkPersistentFlagRequired("remote-path")

	rootCmd.Run = runMain

	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("Failed to execute command")
	}
}

func runMain(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(mountPoint, "DCIM")
		log.Debug().Str("sourceDir", sourceDir).Msg("Inferred sourceDir from mountPoint + /DCIM")
	}
	if verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	if mountType != "" {
		log.Info().Str("drive", mountDrive).Str("mount_point", mountPoint).Str("type", mountType).Msg("Mounting drive")
		mountCmd := exec.Command("sudo", "mount", "-t", mountType, mountDrive, mountPoint, "-o", fmt.Sprintf("uid=%d,gid=%d,metadata", os.Getuid(), os.Getgid()))
		mountCmd.Stdout = os.Stdout
		mountCmd.Stderr = os.Stderr
		if err := mountCmd.Run(); err != nil {
			log.Fatal().Err(err).Msg("Failed to mount drive")
		}
		log.Info().Msg("Drive mounted successfully.")
	} else {
		log.Info().Msg("Skipping mount step (mount-type is empty)")
	}

	if err := organisePhotos(sourceDir, dryRun); err != nil {
		log.Fatal().Err(err).Msg("Failed to organise photos")
	}

	shortFlags := "-avhP"
	if dryRun {
		shortFlags = "-avhPn"
	}

	// Ensure sourceDir ends with a trailing slash for rsync to copy contents, not the directory itself
	rsyncSource := sourceDir
	if !strings.HasSuffix(rsyncSource, string(os.PathSeparator)) {
		rsyncSource += string(os.PathSeparator)
	}
	rsArgs := []string{shortFlags, "--rsync-path=/bin/rsync", "--ignore-existing", rsyncSource, fmt.Sprintf("%s@%s:%s", remoteUser, remoteHost, remotePath)}
	log.Info().Strs("args", rsArgs).Msg("Starting rsync to remote destination...")
	rsCmd := exec.Command("rsync", rsArgs...)
	rsCmd.Stdout = os.Stdout
	rsCmd.Stderr = os.Stderr
	if err := rsCmd.Run(); err != nil {
		log.Fatal().Err(err).Msg("rsync failed")
	}
	log.Info().Msg("rsync completed successfully.")

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

	if mountType != "" {
		log.Info().Str("mount_point", mountPoint).Msg("Unmounting drive")
		umountCmd := exec.Command("sudo", "umount", mountPoint)
		umountCmd.Stdout = os.Stdout
		umountCmd.Stderr = os.Stderr
		if err := umountCmd.Run(); err != nil {
			log.Fatal().Err(err).Msg("Failed to unmount drive")
		}
		log.Info().Msg("Drive unmounted successfully.")
	} else {
		log.Info().Msg("Skipping unmount step (mount-type is empty)")
	}
}

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
