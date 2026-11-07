/*
photo-organiser imports photos from a camera card, groups them by the date they
were taken, and sends each day to Immich or to a remote directory over rsync.

It recognises the card layout on its own (Sony, DJI, Canon, Kodak Charmera), so
the usual invocation is just:

	photo-organiser

Settings that never change between runs live in a config file written by
"photo-organiser init" (default ~/.config/photo-organiser/config.toml). Flags
override environment variables, which override the file.

Available Commands:

	init        Write the config file, prompting for each setting
	stack       Run immich-stack against the configured Immich server
	sync        Trigger an immich library scan
	update      Update photo-organiser to the latest release
	version     Print version information

Flags:

	-s, --source string        source directory, overriding the layout's default
	    --camera string        card layout to assume instead of detecting it (sony, dji, canon, charmera)
	    --config string        config file (default ~/.config/photo-organiser/config.toml)
	    --device string        device to mount (default "/dev/sdd1")
	    --directory string     mount point (default "/dev/camera")
	    --mount-type string    filesystem type for mounting; empty skips mounting (default "exfat")
	    --server string        immich api base url, e.g. https://immich.local/api (env: IMMICH_SERVER)
	    --key string           immich api key (env: IMMICH_API_KEY)
	    --library string       immich library to scan after uploading
	    --host string          remote host for rsync
	    --user string          remote user for rsync (default "$USER")
	    --remote-path string   remote destination path for rsync
	    --stack-binary string  immich-stack binary, for the stack command (default "immich-stack")
	-v, --verbose              enable debug logging
	-n, --dry-run              will not move files, copy them to the remote, or cleanup source directories
*/
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// httpClient serves control-plane calls. It bounds connection setup and the wait
// for response headers without capping total transfer time, so large downloads
// still succeed while a dead server or stalled connection fails fast.
var httpClient = newHTTPClient(30 * time.Second)

// uploadClient is far more patient about response headers: Immich hashes and
// stores a 40MB RAW before it replies, which routinely outlasts the
// control-plane budget on a loaded server.
var uploadClient = newHTTPClient(5 * time.Minute)

func newHTTPClient(responseHeaderTimeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: responseHeaderTimeout,
		},
	}
}

var (
	configPath    string
	sourceDir     string
	cameraName    string
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
	stackBinary   string
)

type ImmichError struct {
	Message    string `json:"message"`
	ErrType    string `json:"error"`
	StatusCode int    `json:"statusCode"`
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	if err := newRootCmd().Execute(); err != nil {
		log.Fatal().Err(err).Msg("Failed to execute command")
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "photo-organiser",
		Short: "Import camera photos into Immich or a remote directory, grouped by the date they were taken.",
		Long: `photo-organiser imports photos from a camera card, groups them by the date they
were taken, and sends each day to Immich or to a remote directory over rsync.
It recognises Sony, DJI, Canon and Kodak Charmera cards on its own.

Run "photo-organiser init" once to write the config file; after that a plain
"photo-organiser" imports whatever card is plugged in.`,
		Args: cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			configureLogging()
			return applyConfig(cmd.Root().PersistentFlags(), configPath)
		},
		Run: runImport,
	}

	rootCmd.Flags().StringVarP(&sourceDir, "source", "s", "", "source directory, overriding the layout's default")
	rootCmd.Flags().StringVar(&cameraName, "camera", "", "card layout to assume instead of detecting it ("+layoutNames()+")")
	rootCmd.Flags().SortFlags = false

	flags := rootCmd.PersistentFlags()
	flags.StringVar(&configPath, "config", defaultConfigPath(), "config file")
	flags.StringVar(&device, "device", "/dev/sdd1", "device to mount")
	flags.StringVar(&directory, "directory", "/dev/camera", "mount point")
	flags.StringVar(&mountType, "mount-type", "exfat", "filesystem type for mounting; empty skips mounting")
	flags.StringVar(&immichServer, "server", "", "immich api base url, e.g. https://immich.local/api (env: IMMICH_SERVER)")
	flags.StringVar(&immichKey, "key", "", "immich api key (env: IMMICH_API_KEY)")
	flags.StringVar(&immichLibrary, "library", "", "immich library to scan after uploading")
	flags.StringVar(&remoteHost, "host", "", "remote host for rsync")
	flags.StringVar(&remoteUser, "user", os.Getenv("USER"), "remote user for rsync")
	flags.StringVar(&remotePath, "remote-path", "", "remote destination path for rsync")
	flags.StringVar(&stackBinary, "stack-binary", "immich-stack", "immich-stack binary, for the stack command")
	flags.BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")
	flags.BoolVarP(&dryRun, "dry-run", "n", false, "will not move files, copy them to the remote, or cleanup source directories")
	flags.SortFlags = false

	rootCmd.AddCommand(
		newInitCmd(),
		newStackCmd(),
		&cobra.Command{Use: "sync", Short: "Trigger an immich library scan", Args: cobra.NoArgs, Run: runSyncCmd},
		&cobra.Command{Use: "version", Short: "Print version information", Args: cobra.NoArgs, Run: runVersion},
		&cobra.Command{Use: "update", Short: "Update photo-organiser to the latest release", Args: cobra.NoArgs, Run: runUpdate},
	)
	return rootCmd
}

func configureLogging() {
	if verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}

func immichConfigured() bool { return immichServer != "" && immichKey != "" }
func rsyncConfigured() bool  { return remoteHost != "" && remotePath != "" }

// runImport mounts the card, works out whose it is, transfers every source the
// layout defines, and offers to clear the card afterwards.
func runImport(cmd *cobra.Command, args []string) {
	mountDrive()

	layout, err := pickLayout(directory, cameraName)
	if err != nil {
		unmountDrive()
		log.Fatal().Err(err).Msg("cannot determine card layout")
	}
	log.Info().Str("camera", layout.name).Msg("card layout")

	sources := resolveSources(layout, directory, sourceDir)
	if len(sources) == 0 {
		unmountDrive()
		log.Fatal().Str("camera", layout.name).Str("root", directory).Msg("none of the layout's source directories exist on the card")
	}

	var transferred []activeSource
	var uploaded bool
	for _, source := range sources {
		groups, err := source.group(source.path)
		if err != nil {
			log.Fatal().Err(err).Str("source", source.name).Msg("failed to group files by date")
		}
		switch {
		case source.rsyncOnly && !rsyncConfigured():
			log.Warn().Str("source", source.name).Msg("skipping: needs rsync, but host and remote_path are not configured")
			continue
		case source.rsyncOnly, !immichConfigured() && rsyncConfigured():
			rsyncByDate(groups)
		case immichConfigured():
			uploadByDate(groups)
			uploaded = true
		default:
			log.Fatal().Msg("configure either server + key (Immich API) or host + remote_path (rsync); see photo-organiser init")
		}
		transferred = append(transferred, source)
	}
	if len(transferred) == 0 {
		log.Warn().Msg("nothing was transferred")
	}

	if promptAndCleanup(transferred) && layout.clearSonyIndex {
		cleanupSonyCardIndex(directory)
	}

	unmountDrive()

	if uploaded && immichLibrary != "" {
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
	if !immichConfigured() {
		log.Fatal().Msg("provide --server and --key, or set them in the config file")
	}
	if immichLibrary == "" {
		log.Fatal().Msg("provide --library, or set it in the config file")
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
