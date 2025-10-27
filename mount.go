package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/rs/zerolog/log"
)

func mountDriveIfNeeded() {
	if mountType != "" {
		// Ensure mount point exists
		if _, err := os.Stat(directory); os.IsNotExist(err) {
			if err := os.MkdirAll(directory, 0755); err != nil {
				log.Warn().Err(err).Str("mount_point", directory).Msg("Failed to create mount point directory, retrying with sudo")
				mkdirCmd := exec.Command("sudo", "mkdir", "-p", directory)
				mkdirCmd.Stdout = os.Stdout
				mkdirCmd.Stderr = os.Stderr
				if err := mkdirCmd.Run(); err != nil {
					log.Fatal().Err(err).Str("mount_point", directory).Msg("Failed to create mount point directory with sudo")
				}
				log.Info().Str("mount_point", directory).Msg("Created mount point directory with sudo")
			}
			log.Info().Str("mount_point", directory).Msg("Created mount point directory")
		}

		uid := os.Getuid()
		gid := os.Getgid()
		var mountOpts string
		switch mountType {
		case "vfat", "exfat", "msdos", "fat":
			mountOpts = fmt.Sprintf("uid=%d,gid=%d,umask=0022", uid, gid)
		default:
			mountOpts = fmt.Sprintf("uid=%d,gid=%d", uid, gid)
		}

		log.Info().Str("drive", device).Str("mount_point", directory).Str("type", mountType).Msg("Mounting drive")
		mountCmd := exec.Command("sudo", "mount", "-t", mountType, device, directory, "-o", mountOpts)
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

func unmountDriveIfNeeded() {
	if mountType != "" {
		log.Info().Str("mount_point", directory).Msg("Unmounting drive")
		umountCmd := exec.Command("sudo", "umount", "-R", directory)
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
