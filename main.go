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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// httpClient is shared by every network call. It bounds connection setup and the
// wait for response headers without capping total transfer time, so large uploads
// and downloads still succeed while a dead server or stalled connection fails fast.
var httpClient = &http.Client{
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

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
	rootCmd.PersistentFlags().StringVar(&immichKey, "key", os.Getenv("IMMICH_API_KEY"), "immich api key (env: IMMICH_API_KEY)")
	rootCmd.PersistentFlags().StringVar(&immichServer, "server", os.Getenv("IMMICH_SERVER"), "immich api base url (env: IMMICH_SERVER)")
	rootCmd.PersistentFlags().SortFlags = false

	cameraCmds := []struct {
		use   string
		short string
		job   cameraJob
	}{
		{
			use:   "sony",
			short: "Organise Sony camera photos (default)",
			job: cameraJob{
				name:           "sony",
				defaultSource:  func() string { return filepath.Join(directory, "DCIM") },
				group:          groupSonyByDate,
				clearSonyIndex: true,
			},
		},
		{
			use:   "sony-video",
			short: "Transfer Sony camera videos via rsync",
			job: cameraJob{
				name:           "sony-video",
				defaultSource:  func() string { return filepath.Join(directory, "PRIVATE", "M4ROOT", "CLIP") },
				group:          groupSonyVideosByDate,
				flatCleanup:    true,
				clearSonyIndex: true,
				rsyncOnly:      true,
			},
		},
		{
			use:   "dji",
			short: "Organise DJI camera (action/drone) photos",
			job: cameraJob{
				name:          "dji",
				defaultSource: func() string { return filepath.Join(directory, "DCIM", "DJI_001") },
				group:         groupDJIByDate,
				flatCleanup:   true,
			},
		},
		{
			use:   "canon",
			short: "Organise Canon camera photos",
			job: cameraJob{
				name:          "canon",
				defaultSource: func() string { return filepath.Join(directory, "DCIM") },
				group:         groupCanonByDate,
			},
		},
		{
			use:   "charmera",
			short: "Organise Kodak Charmera keychain camera photos",
			job: cameraJob{
				name:          "charmera",
				defaultSource: func() string { return directory },
				group:         groupCharmeraByDate,
				flatCleanup:   true,
			},
		},
	}
	for _, cc := range cameraCmds {
		cameraCmd := &cobra.Command{
			Use:   cc.use,
			Short: cc.short,
			Run:   cc.job.run,
		}
		_ = cameraCmd.MarkPersistentFlagRequired("device")
		_ = cameraCmd.MarkPersistentFlagRequired("directory")
		rootCmd.AddCommand(cameraCmd)
	}

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Trigger an immich sync",
		Run:   runSyncCmd,
	}
	_ = syncCmd.MarkPersistentFlagRequired("library")

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

// cameraJob describes how one camera subcommand locates, transfers, and cleans up files.
type cameraJob struct {
	name           string
	defaultSource  func() string                     // source dir when --source is not given
	group          func(string) ([]dateGroup, error) // group source files by date
	flatCleanup    bool                              // remove loose files rather than whole date directories
	clearSonyIndex bool                              // also clear Sony card index files after cleanup
	rsyncOnly      bool                              // require rsync (no Immich upload, no library scan)
}

func (job cameraJob) run(cmd *cobra.Command, args []string) {
	if sourceDir == "" {
		sourceDir = job.defaultSource()
		log.Debug().Str("sourceDir", sourceDir).Msg("inferred source directory")
	}
	if job.rsyncOnly && (remoteHost == "" || remotePath == "") {
		log.Fatal().Msg("provide --host and --remote-path for rsync")
	}

	mountDrive()

	groups, err := job.group(sourceDir)
	if err != nil {
		log.Fatal().Err(err).Str("camera", job.name).Msg("failed to group files by date")
	}

	if job.rsyncOnly {
		rsyncByDate(groups)
	} else {
		transferPhotos(groups)
	}

	var cleaned bool
	if job.flatCleanup {
		cleaned = promptAndCleanupFlat()
	} else {
		cleaned = promptAndCleanup()
	}
	if cleaned && job.clearSonyIndex {
		cleanupSonyCardIndex(directory)
	}

	unmountDrive()

	if !job.rsyncOnly && immichKey != "" && immichServer != "" && immichLibrary != "" {
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
	if immichKey == "" {
		log.Fatal().Msg("provide --key or set IMMICH_API_KEY")
	}
	if immichServer == "" {
		log.Fatal().Msg("provide --server or set IMMICH_SERVER")
	}
	triggerSync()
}

func triggerSync() {
	url := immichServer + "/libraries/" + immichLibrary + "/scan"
	log.Debug().Str("url", url).Msg("Making request to server")
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create http request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", immichKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to perform http request")
	}
	defer func() { _ = resp.Body.Close() }()

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
