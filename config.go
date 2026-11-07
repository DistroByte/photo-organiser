package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
)

// configEntry ties a config file key to the flag it fills, the environment
// variable that can override it, and the wording init uses to ask for it.
// Precedence is flag, then env, then file.
type configEntry struct {
	key    string
	flag   string
	env    string
	prompt string
	secret bool
}

var configEntries = []configEntry{
	{key: "server", flag: "server", env: "IMMICH_SERVER", prompt: "Immich API base URL"},
	{key: "key", flag: "key", env: "IMMICH_API_KEY", prompt: "Immich API key", secret: true},
	{key: "library", flag: "library", prompt: "Immich library to scan after uploading"},
	{key: "host", flag: "host", prompt: "rsync host"},
	{key: "user", flag: "user", prompt: "rsync user"},
	{key: "remote_path", flag: "remote-path", prompt: "rsync destination path"},
	{key: "device", flag: "device", prompt: "card device to mount"},
	{key: "directory", flag: "directory", prompt: "mount point"},
	{key: "mount_type", flag: "mount-type", prompt: "filesystem type (empty skips mounting)"},
}

// defaultConfigPath follows the XDG convention on every platform. os.UserConfigDir
// would put the file under ~/Library/Application Support on macOS, where nobody
// expects to find a CLI tool's config.
func defaultConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "photo-organiser", "config.toml")
}

// applyConfig fills every flag the user did not pass from its environment
// variable, then from the config file. A missing config file is not an error.
func applyConfig(flags *pflag.FlagSet, path string) error {
	values, err := readConfig(path)
	if err != nil {
		return err
	}
	for _, entry := range configEntries {
		flag := flags.Lookup(entry.flag)
		if flag == nil || flag.Changed {
			continue
		}
		value, ok := values[entry.key]
		if entry.env != "" {
			if env := os.Getenv(entry.env); env != "" {
				value, ok = env, true
			}
		}
		if !ok {
			continue
		}
		if err := flag.Value.Set(value); err != nil {
			return fmt.Errorf("applying %s: %w", entry.key, err)
		}
	}
	return nil
}

func readConfig(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	if info, err := f.Stat(); err == nil && info.Mode().Perm()&0o077 != 0 {
		log.Warn().Str("path", path).Msg("config file is readable by other users; run chmod 600 on it")
	}

	values, err := parseConfig(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for key := range values {
		if !knownConfigKey(key) {
			log.Warn().Str("key", key).Str("path", path).Msg("ignoring unknown config key")
		}
	}
	return values, nil
}

func knownConfigKey(key string) bool {
	for _, entry := range configEntries {
		if entry.key == key {
			return true
		}
	}
	return false
}

// parseConfig reads the flat subset of TOML that writeConfig emits: one
// `key = "value"` per line, blank lines and # comments. Tables and arrays are
// rejected rather than ignored so a config that looks right either works or
// fails loudly. Unquoted values are accepted as a convenience for hand edits.
func parseConfig(r io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return nil, fmt.Errorf("line %d: tables are not supported", lineNo)
		}
		key, rawValue, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("line %d: expected key = value", lineNo)
		}
		if _, dup := values[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q", lineNo, key)
		}
		value, err := parseConfigValue(strings.TrimSpace(rawValue))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		values[key] = value
	}
	return values, scanner.Err()
}

func parseConfigValue(raw string) (string, error) {
	switch {
	case raw == "":
		return "", errors.New("missing value")
	case raw[0] == '"':
		end := closingQuote(raw)
		if end < 0 {
			return "", errors.New("unterminated string")
		}
		value, err := strconv.Unquote(raw[:end+1])
		if err != nil {
			return "", fmt.Errorf("invalid string %s", raw)
		}
		return value, checkTrailing(raw[end+1:])
	case raw[0] == '\'':
		end := strings.IndexByte(raw[1:], '\'')
		if end < 0 {
			return "", errors.New("unterminated string")
		}
		return raw[1 : end+1], checkTrailing(raw[end+2:])
	default:
		value, _, _ := strings.Cut(raw, "#")
		return strings.TrimSpace(value), nil
	}
}

// closingQuote returns the index of the double quote that ends a basic string,
// skipping any that are backslash-escaped, or -1 if the string never closes.
func closingQuote(raw string) int {
	for i := 1; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

func checkTrailing(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasPrefix(rest, "#") {
		return nil
	}
	return fmt.Errorf("unexpected text after value: %s", rest)
}

// writeConfig writes every known key, quoted, so the file is valid TOML and
// shows which settings exist. It holds the API key, so the file is readable
// only by its owner.
func writeConfig(path string, values map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	// OpenFile only applies the mode to a new file; a pre-existing one keeps
	// whatever permissions it had.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}

	var sb strings.Builder
	sb.WriteString("# photo-organiser configuration.\n")
	sb.WriteString("# Flags and the IMMICH_SERVER / IMMICH_API_KEY environment variables override these values.\n\n")
	for _, entry := range configEntries {
		fmt.Fprintf(&sb, "%s = %s\n", entry.key, strconv.Quote(values[entry.key]))
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// promptConfig asks for each setting in turn, showing the current value as the
// default so Enter keeps it. Answers replace entries in values.
func promptConfig(in io.Reader, out io.Writer, values map[string]string) error {
	reader := bufio.NewReader(in)
	for _, entry := range configEntries {
		current := values[entry.key]
		shown := current
		if entry.secret {
			shown = maskSecret(current)
		}
		_, _ = fmt.Fprintf(out, "%s [%s]: ", entry.prompt, shown)

		answer, err := readAnswer(reader, in, out, entry.secret)
		if err != nil {
			return err
		}
		if answer != "" {
			values[entry.key] = answer
		}
	}
	return nil
}

// readAnswer reads one line, suppressing terminal echo for secrets when the
// input really is the user's terminal. EOF counts as an empty answer so a
// piped or truncated input keeps the defaults rather than failing.
func readAnswer(reader *bufio.Reader, in io.Reader, out io.Writer, secret bool) (string, error) {
	if secret && isTerminal(in) {
		if restore := withoutEcho(out); restore != nil {
			defer restore()
		}
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// withoutEcho turns terminal echo off and returns the function that turns it
// back on, or nil if echo could not be changed. An interrupt while echo is off
// would otherwise leave the shell blind, since a signal skips deferred calls,
// so one is caught, echo restored, and the process exits as if uncaught.
func withoutEcho(out io.Writer) func() {
	if err := setEcho(false); err != nil {
		return nil
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	go func() {
		if _, ok := <-interrupts; ok {
			_ = setEcho(true)
			_, _ = fmt.Fprintln(out)
			os.Exit(130)
		}
	}()
	return func() {
		signal.Stop(interrupts)
		close(interrupts)
		_ = setEcho(true)
		_, _ = fmt.Fprintln(out)
	}
}

func isTerminal(r io.Reader) bool {
	file, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// setEcho shells out to stty rather than pulling in x/term; the tool only
// ships for Linux and macOS, where stty is always present.
func setEcho(on bool) error {
	arg := "-echo"
	if on {
		arg = "echo"
	}
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// maskSecret keeps just enough of a key to recognise it in a prompt.
func maskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "****"
	}
	return "****" + secret[len(secret)-4:]
}
