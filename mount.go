package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/rs/zerolog/log"
)

// mountDriveIfNeeded mounts the specified drive to the mount point if mountType is set.
func mountDriveIfNeeded() {
	if mountType != "" {
		// Ensure mount point exists
		if _, err := os.Stat(mountPoint); os.IsNotExist(err) {
			if err := os.MkdirAll(mountPoint, 0755); err != nil {
				log.Warn().Err(err).Str("mount_point", mountPoint).Msg("Failed to create mount point directory, retrying with sudo")
				mkdirCmd := exec.Command("sudo", "mkdir", "-p", mountPoint)
				mkdirCmd.Stdout = os.Stdout
				mkdirCmd.Stderr = os.Stderr
				if err := mkdirCmd.Run(); err != nil {
					log.Fatal().Err(err).Str("mount_point", mountPoint).Msg("Failed to create mount point directory with sudo")
				}
				log.Info().Str("mount_point", mountPoint).Msg("Created mount point directory with sudo")
			}
			log.Info().Str("mount_point", mountPoint).Msg("Created mount point directory")
		}
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
}

// unmountDriveIfNeeded unmounts the drive from the mount point if mountType is set.
func unmountDriveIfNeeded() {
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
