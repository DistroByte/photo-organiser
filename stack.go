package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

func newStackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stack [immich-stack arguments]",
		Short: "Run immich-stack against the configured Immich server",
		Long: `Runs the immich-stack binary with API_URL and API_KEY taken from the
photo-organiser config, so stacking, duplicates and fix-trash use the same
server and key as uploads. Every argument is passed straight through:

  photo-organiser stack --dry-run
  photo-organiser stack duplicates
  photo-organiser stack fix-trash --trash-orphaned-raws

The binary is found via the stack_binary config key (default: immich-stack on PATH).`,
		// Flags belong to immich-stack, not to us, so cobra must not eat them.
		DisableFlagParsing: true,
		Run:                runStack,
	}
}

func runStack(cmd *cobra.Command, args []string) {
	if !immichConfigured() {
		log.Fatal().Msg("provide --server and --key, or set them in the config file")
	}
	stack, err := stackCommand(stackBinary, args)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot run immich-stack")
	}
	stack.Stdin, stack.Stdout, stack.Stderr = os.Stdin, os.Stdout, os.Stderr

	err = stack.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	if err != nil {
		log.Fatal().Err(err).Msg("immich-stack failed to start")
	}
}

// stackCommand builds the immich-stack invocation. The key travels in the
// environment rather than argv so it never shows up in ps or shell history.
func stackCommand(binary string, args []string) (*exec.Cmd, error) {
	path, err := exec.LookPath(expandHome(binary))
	if err != nil {
		return nil, err
	}
	stack := exec.Command(path, args...)
	stack.Env = append(os.Environ(), "API_URL="+immichServer, "API_KEY="+immichKey)
	return stack, nil
}

// expandHome resolves a leading ~ so a config file can hold the same path the
// user types in a shell.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}
