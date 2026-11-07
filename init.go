package main

import (
	"os"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var forceInit bool

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write the config file, prompting for each setting",
		Args:  cobra.NoArgs,
		// An existing config supplies the prompt defaults, but a broken file
		// must not stop init from replacing it.
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			configureLogging()
			if err := applyConfig(cmd.Flags(), configPath); err != nil {
				log.Warn().Err(err).Msg("ignoring existing config")
			}
		},
		Run: runInit,
	}
	cmd.Flags().BoolVar(&forceInit, "force", false, "overwrite an existing config file")
	return cmd
}

func runInit(cmd *cobra.Command, args []string) {
	if _, err := os.Stat(configPath); err == nil && !forceInit {
		log.Fatal().Str("path", configPath).Msg("config already exists; pass --force to overwrite it")
	}

	values := make(map[string]string, len(configEntries))
	for _, entry := range configEntries {
		values[entry.key] = cmd.Flags().Lookup(entry.flag).Value.String()
	}
	if err := promptConfig(os.Stdin, os.Stdout, values); err != nil {
		log.Fatal().Err(err).Msg("reading answers")
	}
	if err := writeConfig(configPath, values); err != nil {
		log.Fatal().Err(err).Msg("writing config")
	}
	log.Info().Str("path", configPath).Msg("config written")
}
