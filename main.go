/*
photo-organiser is a CLI tool that organises camera photos into a directory structure based on the date they were taken.

Available Commands:

	canon       Organise Canon camera photos
	completion  Generate the autocompletion script for the specified shell
	dji         Organise DJI camera (action/drone) photos
	help        Help about any command
	sony        Organise Sony camera photos (default)
	sync        Trigger an immich sync
	update      Update photo-organiser to the latest release
	version     Print version information

Flags:

	    --device string        device to mount (default "/dev/sdd1")
	    --directory string     mount point (default "/dev/camera")
	-n, --dry-run              will not move files, copy them to the remote, or cleanup source directories
	-h, --help                 help for photo-organiser
	    --host string          remote host for rsync
	    --key string           immich api key (use instead of --host/--remote-path for direct upload)
	    --mount-type string    filesystem type for mounting (default "exfat")
	    --remote-path string   remote destination path for rsync
	    --server string        immich api base url (e.g. https://immich.local/api)
	-s, --source string        source directory containing the photos. (default /mount/point/DCIM)
	    --user string          remote user for rsync (default "james")
	-v, --verbose              enable debug logging

Example usage:

	# Upload directly to Immich (no SSH required)
	photo-organiser sony --server https://immich.local/api --key <api-key>

	# Upload via rsync over SSH
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
	rootCmd.PersistentFlags().StringVar(&immichLibrary, "library", "", "library to trigger a scan on")
	rootCmd.PersistentFlags().StringVar(&immichKey, "key", "", "immich api key")
	rootCmd.PersistentFlags().StringVar(&immichServer, "server", "", "immich api base url")
	rootCmd.PersistentFlags().SortFlags = false

	sonyCmd := &cobra.Command{
		Use:   "sony",
		Short: "Organise Sony camera photos (default)",
		Run:   runCameraPhotos,
	}
	sonyCmd.MarkPersistentFlagRequired("device")
	sonyCmd.MarkPersistentFlagRequired("directory")

	djiCmd := &cobra.Command{
		Use:   "dji",
		Short: "Organise DJI camera (action/drone) photos",
		Run:   runDJIPhotos,
	}
	djiCmd.MarkPersistentFlagRequired("device")
	djiCmd.MarkPersistentFlagRequired("directory")

	canonCmd := &cobra.Command{
		Use:   "canon",
		Short: "Organise Canon camera photos",
		Run:   runCanonPhotos,
	}
	canonCmd.MarkPersistentFlagRequired("device")
	canonCmd.MarkPersistentFlagRequired("directory")

	charmeraCmd := &cobra.Command{
		Use:   "charmera",
		Short: "Organise Kodak Charmera keychain camera photos",
		Run:   runCharmeraPhotos,
	}
	charmeraCmd.MarkPersistentFlagRequired("device")
	charmeraCmd.MarkPersistentFlagRequired("directory")

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Trigger an immich sync",
		Run:   runSyncCmd,
	}
	syncCmd.MarkPersistentFlagRequired("library")
	syncCmd.MarkPersistentFlagRequired("key")
	syncCmd.MarkPersistentFlagRequired("server")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run:   runVersion,
	}

	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update photo-organiser to the latest release",
		Run:   runUpdate,
	}

	rootCmd.AddCommand(sonyCmd)
	rootCmd.AddCommand(djiCmd)
	rootCmd.AddCommand(canonCmd)
	rootCmd.AddCommand(charmeraCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	}

	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("Failed to execute command")
	}
}

// transferPhotos uploads or syncs a set of date groups using the configured mode.
// Immich API mode is used when --server and --key are set; rsync is the fallback.
func transferPhotos(groups []dateGroup) {
	if immichServer != "" && immichKey != "" {
		uploadByDate(groups)
	} else if remoteHost != "" && remotePath != "" {
		rsyncByDate(groups)
	} else {
		log.Fatal().Msg("provide either --server + --key (Immich API) or --host + --remote-path (rsync)")
	}
}

func runCameraPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(directory, "DCIM")
		log.Debug().Str("sourceDir", sourceDir).Msg("inferred sourceDir from mount point + /DCIM")
	}
	mountDrive()
	groups, err := groupSonyByDate(sourceDir)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to group Sony photos by date")
	}
	transferPhotos(groups)
	promptAndCleanup()
	unmountDrive()

	if immichKey != "" && immichServer != "" && immichLibrary != "" {
		triggerSync()
	}
}

func runDJIPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(directory, "DCIM", "DJI_001")
		log.Debug().Str("sourceDir", sourceDir).Msg("inferred sourceDir for DJI camera")
	}
	mountDrive()
	groups, err := groupDJIByDate(sourceDir)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to group DJI photos by date")
	}
	transferPhotos(groups)
	promptAndCleanupFlat()
	unmountDrive()

	if immichKey != "" && immichServer != "" && immichLibrary != "" {
		triggerSync()
	}
}

func runCanonPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = filepath.Join(directory, "DCIM")
		log.Debug().Str("sourceDir", sourceDir).Msg("inferred sourceDir from mount point + /DCIM")
	}
	mountDrive()
	groups, err := groupCanonByDate(sourceDir)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to group Canon photos by date")
	}
	transferPhotos(groups)
	promptAndCleanup()
	unmountDrive()

	if immichKey != "" && immichServer != "" && immichLibrary != "" {
		triggerSync()
	}
}

func runCharmeraPhotos(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = directory
		log.Debug().Str("sourceDir", sourceDir).Msg("inferred sourceDir from mount point root")
	}
	mountDrive()
	groups, err := groupCharmeraByDate(sourceDir)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to group Charmera photos by date")
	}
	transferPhotos(groups)
	promptAndCleanupFlat()
	unmountDrive()

	if immichKey != "" && immichServer != "" && immichLibrary != "" {
		triggerSync()
	}
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
	triggerSync()
}

func triggerSync() {
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
