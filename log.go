package main

import "github.com/rs/zerolog"

// setLogLevel sets the global log level to debug if verbose is true, info otherwise.
func setLogLevel() {
	if verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
