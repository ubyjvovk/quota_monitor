package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"quotamon/internal/config"
	"quotamon/internal/discover"
)

func runDiscover(args []string, stdout, stderr io.Writer, allFindings func() []discover.Finding) int {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--json") {
		fmt.Fprint(stderr, usageText)
		return 2
	}

	findings := allFindings()
	if len(args) == 0 {
		fmt.Fprint(stdout, renderFindings(findings, nil))
		return 0
	}
	if err := json.NewEncoder(stdout).Encode(findings); err != nil {
		fmt.Fprintf(stderr, "encode discovery: %v\n", err)
		return 1
	}
	return 0
}

func runConfig(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "path":
		if len(args) != 1 {
			fmt.Fprint(stderr, usageText)
			return 2
		}
		fmt.Fprintln(stdout, config.Path())
		return 0
	case "get":
		return runConfigGet(args[1:], stdout, stderr)
	case "set":
		return runConfigSet(args[1:], stdin, stdout, stderr)
	default:
		fmt.Fprint(stderr, usageText)
		return 2
	}
}

func runConfigGet(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--json") {
		fmt.Fprint(stderr, usageText)
		return 2
	}

	cfg, err := effectiveConfig()
	if err != nil {
		fmt.Fprintf(stderr, "config: %v\n", err)
		return 3
	}
	if len(args) == 1 {
		if err := json.NewEncoder(stdout).Encode(redactConfig(cfg)); err != nil {
			fmt.Fprintf(stderr, "encode config: %v\n", err)
			return 1
		}
		return 0
	}

	ids := make([]string, 0, len(cfg.Providers))
	for id := range cfg.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		provider := cfg.Providers[id]
		state := "off"
		if provider.Enabled {
			state = "on"
		}
		detail := make([]string, 0, 2)
		if provider.Live != "" {
			detail = append(detail, "live="+provider.Live)
		}
		if provider.APIKey != "" {
			detail = append(detail, "api key set")
		}
		if len(detail) == 0 {
			detail = append(detail, "defaults")
		}
		fmt.Fprintf(stdout, "%s: %s (%s)\n", id, state, strings.Join(detail, ", "))
	}
	return 0
}

// redactedConfig is the machine-readable shape of `config get --json`: every
// field a frontend needs to render setup, with API keys replaced by an
// existence boolean. Key values never leave the file, so this view is safe to
// print. It is built fresh from the loaded config; nothing is mutated.
type redactedConfig struct {
	Version   int                         `json:"version"`
	Providers map[string]redactedProvider `json:"providers"`
}

type redactedProvider struct {
	Enabled   bool   `json:"enabled"`
	Live      string `json:"live,omitempty"`
	APIKeySet bool   `json:"api_key_set"`
}

func redactConfig(cfg config.Config) redactedConfig {
	redacted := redactedConfig{Version: cfg.Version, Providers: make(map[string]redactedProvider, len(cfg.Providers))}
	for id, provider := range cfg.Providers {
		redacted.Providers[id] = redactedProvider{
			Enabled:   provider.Enabled,
			Live:      provider.Live,
			APIKeySet: provider.APIKey != "",
		}
	}
	return redacted
}

func effectiveConfig() (config.Config, error) {
	effective := config.Default()
	loaded, err := config.Load()
	if errors.Is(err, config.ErrMissing) {
		return effective, nil
	}
	if err != nil {
		return config.Config{}, err
	}
	effective.Version = loaded.Version
	for id, provider := range loaded.Providers {
		effective.Providers[id] = provider
	}
	return effective, nil
}

type configSetOptions struct {
	enabled     bool
	hasEnabled  bool
	apiKey      string
	hasAPIKey   bool
	apiKeyStdin bool
	live        string
	hasLive     bool
}

func runConfigSet(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	id := args[0]
	if _, known := config.Default().Providers[id]; !known {
		fmt.Fprintf(stderr, "unknown provider %q\n", id)
		return 2
	}
	options, valid := parseConfigSetOptions(args[1:])
	if !valid {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	if options.apiKeyStdin {
		key, err := readStdinKey(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "config: %v\n", err)
			return 2
		}
		options.apiKey = key
		options.hasAPIKey = true
	}
	if !options.hasEnabled && !options.hasAPIKey && !options.hasLive {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	if options.hasLive && (id != "codex" || !validCodexLiveMode(options.live)) {
		fmt.Fprint(stderr, usageText)
		return 2
	}

	cfg, err := config.Load()
	if errors.Is(err, config.ErrMissing) {
		cfg = config.Default()
	} else if err != nil {
		fmt.Fprintf(stderr, "config: %v\n", err)
		return 3
	}
	provider := cfg.Providers[id]
	if options.hasEnabled {
		provider.Enabled = options.enabled
	}
	if options.hasAPIKey {
		provider.APIKey = options.apiKey
	}
	if options.hasLive {
		provider.Live = options.live
	}
	cfg.Providers[id] = provider
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(stderr, "config: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", config.Path())
	return 0
}

func parseConfigSetOptions(args []string) (configSetOptions, bool) {
	var result configSetOptions
	for _, argument := range args {
		// --api-key-stdin is the only flag without a value; it names which
		// secret source to use, so providing a key in argv at the same time is
		// rejected as ambiguous.
		if argument == "--api-key-stdin" {
			if result.hasAPIKey || result.apiKeyStdin {
				return configSetOptions{}, false
			}
			result.apiKeyStdin = true
			continue
		}
		name, value, found := strings.Cut(argument, "=")
		if !found {
			return configSetOptions{}, false
		}
		switch name {
		case "--enabled":
			if result.hasEnabled || (value != "true" && value != "false") {
				return configSetOptions{}, false
			}
			result.enabled, _ = strconv.ParseBool(value)
			result.hasEnabled = true
		case "--api-key":
			if result.hasAPIKey || result.apiKeyStdin {
				return configSetOptions{}, false
			}
			result.apiKey = value
			result.hasAPIKey = true
		case "--live":
			if result.hasLive {
				return configSetOptions{}, false
			}
			result.live = value
			result.hasLive = true
		default:
			return configSetOptions{}, false
		}
	}
	return result, true
}

// readStdinKey reads the API key as the first line of stdin, trimming exactly
// one trailing newline. Piping the secret instead of placing it in argv keeps
// it out of `ps` process listings; an empty line is an error because there is
// nothing worth saving.
func readStdinKey(stdin io.Reader) (string, error) {
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read API key from stdin: %w", err)
	}
	line = strings.TrimSuffix(line, "\n")
	if line == "" {
		return "", errors.New("API key from stdin must not be empty")
	}
	return line, nil
}

func validCodexLiveMode(mode string) bool {
	switch mode {
	case "app-server", "http", "off":
		return true
	default:
		return false
	}
}
