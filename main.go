/*
photo-organiser is a CLI tool that organises camera photos into a directory structure based on the date they were taken.

Available Commands:

	canon       Organise Canon camera photos
	completion  Generate the autocompletion script for the specified shell
	dji         Organise DJI camera (action/drone) photos
	help        Help about any command
	sony        Organise Sony camera photos (default)
	sync        Trigger an immich sync
	version     Print version information

Flags:

	    --device string        device to mount (default "/dev/sdd1")
	    --directory string     mount point (default "/dev/camera")
	-n, --dry-run              will not move files, copy them to the remote, or cleanup source directories
	-h, --help                 help for photo-organiser
	    --host string          remote host for rsync
	    --mount-type string    filesystem type for mounting (default "exfat")
	    --remote-path string   remote destination path for rsync
	-s, --source string        source directory containing the photos. (default /mount/point/DCIM)
	    --user string          remote user for rsync (default "james")
	-v, --verbose              enable debug logging

Example usage:

	photo-organiser sony --host remote.host --user username --remote-path /path/on/remote
*/
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	sourceDir     string
	dryRun        bool
	verbose       bool
	remoteUser    string
	remoteHost    string
	remotePath    string
	device        string
	directory     string
	mountType     string
	immichLibrary string
	immichKey     string
	immichServer  string
)

type ImmichError struct {
	Message    string `json:"message"`
	ErrType    string `json:"error"`
	StatusCode int    `json:"statusCode"`
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	rootCmd := &cobra.Command{
		Use:   "photo-organiser",
		Short: "Organise camera photos into a directory structure based on the date they were taken.",
		Long:  `photo-organiser is a CLI tool that organises camera photos into a directory structure based on the date they were taken.`,

		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				zerolog.SetGlobalLevel(zerolog.DebugLevel)
			} else {
				zerolog.SetGlobalLevel(zerolog.InfoLevel)
			}
		},
	}

	rootCmd.PersistentFlags().StringVar(&device, "device", "/dev/sdd1", "device to mount")
	rootCmd.PersistentFlags().StringVar(&directory, "directory", "/dev/camera", "mount point")
	rootCmd.PersistentFlags().StringVarP(&sourceDir, "source", "s", "", "source directory containing the photos. (default /mount/point/DCIM)")
	rootCmd.PersistentFlags().StringVar(&remoteUser, "user", os.Getenv("USER"), "remote user for rsync")
	rootCmd.PersistentFlags().StringVar(&remoteHost, "host", "", "remote host for rsync")
	rootCmd.PersistentFlags().StringVar(&remotePath, "remote-path", "", "remote destination path for rsync")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "will not move files, copy them to the remote, or cleanup source directories")
	rootCmd.PersistentFlags().StringVar(&mountType, "mount-type", "exfat", "filesystem type for mounting")
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

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Trigger an immich sync",
		Run:   runSyncCmd,
	}
	syncCmd.Flags().StringVar(&immichLibrary, "library", "", "library to trigger a scan on")
	syncCmd.Flags().StringVar(&immichKey, "key", "", "immich api key")
	syncCmd.Flags().StringVar(&immichServer, "server", "", "immich api base url")
	syncCmd.MarkFlagRequired("library")
	syncCmd.MarkFlagRequired("key")
	syncCmd.MarkFlagRequired("server")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run:   runVersion,
	}

	rootCmd.AddCommand(sonyCmd)
	rootCmd.AddCommand(djiCmd)
	rootCmd.AddCommand(canonCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(versionCmd)
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
	mountDrive()
	if err := organiseSonyPhotos(sourceDir, dryRun); err != nil {
		log.Fatal().Err(err).Msg("Failed to organise photos")
	}
	rsyncToRemote()
	promptAndCleanup()
	unmountDrive()
}

func runDJIPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(directory, "DCIM", "DJI_001")
		log.Debug().Str("sourceDir", sourceDir).Msg("Inferred sourceDir for DJI camera")
	}
	mountDrive()
	if err := organiseDJIPhotos(sourceDir, dryRun); err != nil {
		log.Fatal().Err(err).Msg("Failed to organise DJI photos")
	}
	rsyncToRemote()
	promptAndCleanup()
	unmountDrive()
}

func runCanonPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(directory, "DCIM")
		log.Debug().Str("sourceDir", sourceDir).Msg("Inferred sourceDir from mountPoint + /DCIM")
	}
	mountDrive()
	if err := organiseCanonPhotos(sourceDir, dryRun); err != nil {
		log.Fatal().Err(err).Msg("Failed to organise photos")
	}
	rsyncToRemote()
	promptAndCleanup()
	unmountDrive()
}

func runVersion(cmd *cobra.Command, args []string) {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("Unable to determine version information.")
		return
	}

	if buildInfo.Main.Version != "" {
		fmt.Printf("photo-organiser version %s\n", buildInfo.Main.Version)
	} else {
		fmt.Println("photo-organiser version unknown")
	}
}

func runSyncCmd(cmd *cobra.Command, args []string) {
	url := immichServer + "/libraries/" + immichLibrary + "/scan"
	log.Debug().Str("url", url).Msg("Making request to server")
	client := &http.Client{}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create http request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", immichKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to perform http request")
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 204 {
		var apiErr ImmichError
		if err := json.Unmarshal(bodyBytes, &apiErr); err == nil {
			log.Fatal().
				Int("status", apiErr.StatusCode).
				Str("url", url).
				Str("error", apiErr.Message).
				Err(&apiErr).
				Msg("Failed to trigger scan")
		} else {
			// fallback if response isn't the expected JSON
			log.Fatal().
				Int("status", resp.StatusCode).
				Str("url", url).
				Str("http_body", string(bodyBytes)).
				Msg("Failed to trigger scan")
		}
	}

	log.Info().Msg("sync triggered successfully")
}

func (e *ImmichError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}
