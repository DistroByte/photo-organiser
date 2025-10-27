package main

import (
	"os"
	"path/filepath"

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
	device     string
	directory  string
	mountType  string
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	rootCmd := &cobra.Command{
		Use:   "photo-organiser",
		Short: "Organise camera photos into a directory structure based on the date they were taken.",
		Long:  `photo-organiser is a CLI tool that organises camera photos into a directory structure based on the date they were taken.`,
	}

	rootCmd.PersistentFlags().StringVar(&device, "device", "", "Drive to mount (e.g. f:)")
	rootCmd.PersistentFlags().StringVar(&directory, "directory", "/dev/sdd", "Mount point (e.g. /mnt/f)")
	rootCmd.PersistentFlags().StringVarP(&sourceDir, "source", "", "", "Source directory containing the photos, defaults to /mount/point/DCIM")
	rootCmd.PersistentFlags().StringVar(&remoteUser, "user", os.Getenv("USER"), "Remote user for rsync, defaults to current user")
	rootCmd.PersistentFlags().StringVar(&remoteHost, "host", "", "Remote host for rsync")
	rootCmd.PersistentFlags().StringVar(&remotePath, "remote-path", "", "Remote destination path for rsync")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "If set, will not move files but print what it would do")
	rootCmd.PersistentFlags().StringVar(&mountType, "mount-type", "exfat", "Filesystem type for mounting (drvfs for WSL, vfat/exfat for Linux, leave empty to skip mounting)")
	rootCmd.PersistentFlags().SortFlags = false

	sonyCmd := &cobra.Command{
		Use:   "sony",
		Short: "Organise Sony camera photos (default)",
		Run:   runCameraPhotos,
	}
	sonyCmd.MarkPersistentFlagRequired("device")
	sonyCmd.MarkPersistentFlagRequired("directory")
	sonyCmd.MarkPersistentFlagRequired("host")
	sonyCmd.MarkPersistentFlagRequired("remote-path")

	djiCmd := &cobra.Command{
		Use:   "dji",
		Short: "Organise DJI camera (action/drone) photos",
		Run:   runDJIPhotos,
	}
	djiCmd.MarkPersistentFlagRequired("device")
	djiCmd.MarkPersistentFlagRequired("directory")
	djiCmd.MarkPersistentFlagRequired("host")
	djiCmd.MarkPersistentFlagRequired("remote-path")

	canonCmd := &cobra.Command{
		Use:   "canon",
		Short: "Organise Canon camera photos",
		Run:   runCanonPhotos,
	}
	canonCmd.MarkPersistentFlagRequired("device")
	canonCmd.MarkPersistentFlagRequired("directory")
	canonCmd.MarkPersistentFlagRequired("host")
	canonCmd.MarkPersistentFlagRequired("remote-path")

	rootCmd.AddCommand(sonyCmd)
	rootCmd.AddCommand(djiCmd)
	rootCmd.AddCommand(canonCmd)
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	}

	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("Failed to execute command")
	}
}

func runCameraPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(directory, "DCIM")
		log.Debug().Str("sourceDir", sourceDir).Msg("Inferred sourceDir from mountPoint + /DCIM")
	}
	mountDriveIfNeeded()
	if err := organiseSonyPhotos(sourceDir, dryRun); err != nil {
		log.Fatal().Err(err).Msg("Failed to organise photos")
	}
	rsyncToRemote()
	promptAndCleanup()
	unmountDriveIfNeeded()
}

func runDJIPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(directory, "DCIM", "DJI_001")
		log.Debug().Str("sourceDir", sourceDir).Msg("Inferred sourceDir for DJI camera")
	}
	mountDriveIfNeeded()
	if err := organiseDJIPhotos(sourceDir, dryRun); err != nil {
		log.Fatal().Err(err).Msg("Failed to organise DJI photos")
	}
	rsyncToRemote()
	promptAndCleanup()
	unmountDriveIfNeeded()
}

func runCanonPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(directory, "DCIM")
		log.Debug().Str("sourceDir", sourceDir).Msg("Inferred sourceDir from mountPoint + /DCIM")
	}
	mountDriveIfNeeded()
	if err := organiseCanonPhotos(sourceDir, dryRun); err != nil {
		log.Fatal().Err(err).Msg("Failed to organise photos")
	}
	rsyncToRemote()
	promptAndCleanup()
	unmountDriveIfNeeded()
}
