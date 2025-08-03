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
	mountDrive string
	mountPoint string
	mountType  string
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	rootCmd := &cobra.Command{
		Use:   "photo-organiser",
		Short: "Organise camera photos into a directory structure based on the date they were taken.",
		Long:  `photo-organiser is a CLI tool that organises camera photos into a directory structure based on the date they were taken.`,
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

	cameraCmd := &cobra.Command{
		Use:   "camera",
		Short: "Organise Sony camera photos (default)",
		Run:   runCameraPhotos,
	}
	cameraCmd.MarkPersistentFlagRequired("mount-drive")
	cameraCmd.MarkPersistentFlagRequired("mount-point")
	cameraCmd.MarkPersistentFlagRequired("host")
	cameraCmd.MarkPersistentFlagRequired("remote-path")

	djiCmd := &cobra.Command{
		Use:   "dji",
		Short: "Organise DJI camera (action/drone) photos",
		Run:   runDJIPhotos,
	}
	djiCmd.MarkPersistentFlagRequired("mount-drive")
	djiCmd.MarkPersistentFlagRequired("mount-point")
	djiCmd.MarkPersistentFlagRequired("host")
	djiCmd.MarkPersistentFlagRequired("remote-path")

	rootCmd.AddCommand(cameraCmd)
	rootCmd.AddCommand(djiCmd)
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	}

	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("Failed to execute command")
	}
}

// runCameraPhotos runs the workflow for Sony camera photos.
func runCameraPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(mountPoint, "DCIM")
		log.Debug().Str("sourceDir", sourceDir).Msg("Inferred sourceDir from mountPoint + /DCIM")
	}
	mountDriveIfNeeded()
	if err := organisePhotos(sourceDir, dryRun); err != nil {
		log.Fatal().Err(err).Msg("Failed to organise photos")
	}
	rsyncToRemote()
	promptAndCleanup()
	unmountDriveIfNeeded()
}

// runDJIPhotos runs the workflow for DJI camera (action/drone) photos.
func runDJIPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(mountPoint, "DCIM", "DJI_001")
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
