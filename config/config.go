// Package config reads TOML configuration files for termd tools.
//
// The server and termctl share ~/.config/termd/server.toml.
// The frontend uses ~/.config/termd-tui/config.toml, falling back to
// the server config's first listen address if no frontend config exists.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ProgramConfig describes a named program that can be spawned.
type ProgramConfig struct {
	Name string            `toml:"name"`
	Cmd  string            `toml:"cmd"`
	Args []string          `toml:"args"`
	Env  map[string]string `toml:"env"`
}

// SessionsConfig holds session-related server settings.
type SessionsConfig struct {
	DefaultName     string   `toml:"default-name"`
	DefaultPrograms []string `toml:"default-programs"`
}

// ServerConfig represents termd/server.toml.
type ServerConfig struct {
	Listen   []string        `toml:"listen"`
	Debug    bool            `toml:"debug"`
	Programs []ProgramConfig `toml:"programs"`
	SSH      SSHConfig       `toml:"ssh"`
	Termctl  TermctlConfig   `toml:"termctl"`
	Sessions SessionsConfig  `toml:"sessions"`
}

// SSHConfig holds SSH-specific server settings.
type SSHConfig struct {
	HostKey        string `toml:"host-key"`
	AuthorizedKeys string `toml:"authorized-keys"`
	NoAuth         bool   `toml:"no-auth"`
}

// TermctlConfig holds termctl defaults from the server config.
type TermctlConfig struct {
	Connect string `toml:"connect"`
	Debug   bool   `toml:"debug"`
}

// FrontendConfig represents termd-tui/config.toml.
type FrontendConfig struct {
	Connect string `toml:"connect"`
	Debug   bool   `toml:"debug"`
}

// LoadServerConfig reads the server configuration file.
// If explicit is non-empty, that file is used (error if missing).
// Otherwise, checks XDG paths. Returns zero config if no file found.
func LoadServerConfig(explicit string) (ServerConfig, error) {
	var cfg ServerConfig
	path := explicit
	if path == "" {
		path = findConfig("termd", "server.toml")
	}
	if path == "" {
		return cfg, nil
	}
	_, err := toml.DecodeFile(path, &cfg)
	return cfg, err
}

// LoadFrontendConfig reads the frontend configuration file.
// If explicit is non-empty, that file is used (error if missing).
// Otherwise, checks XDG paths. If no frontend config exists, falls
// back to the first listen address from the server config.
// Returns zero config if nothing found.
func LoadFrontendConfig(explicit string) (FrontendConfig, error) {
	var cfg FrontendConfig
	path := explicit
	if path == "" {
		path = findConfig("termd-tui", "config.toml")
	}
	if path != "" {
		_, err := toml.DecodeFile(path, &cfg)
		return cfg, err
	}

	// Fallback: read server config for the connect address
	serverCfg, err := LoadServerConfig("")
	if err != nil {
		return cfg, nil // ignore server config errors for fallback
	}
	if len(serverCfg.Listen) > 0 {
		cfg.Connect = serverCfg.Listen[0]
	}
	return cfg, nil
}

// KeybindConfig represents termd-tui/keybindings.toml.
// Bindings are organized by category: [tab], [session], [main].
// Values are either a single key string or an array of key strings.
// An empty string unbinds the command.
type KeybindConfig struct {
	Style   string         `toml:"style"`
	Prefix  string         `toml:"prefix"`
	Tab     map[string]any `toml:"tab"`
	Session map[string]any `toml:"session"`
	Main    map[string]any `toml:"main"`
}

// Overrides flattens all category bindings into a single map
// of command-invocation -> key-specs.
func (c KeybindConfig) Overrides() map[string][]string {
	result := make(map[string][]string)
	for _, m := range []map[string]any{c.Tab, c.Session, c.Main} {
		for cmd, v := range m {
			result[cmd] = keysFromValue(v)
		}
	}
	return result
}

// keysFromValue extracts key specs from a TOML value that is
// either a string or an array of strings.
func keysFromValue(v any) []string {
	switch v := v.(type) {
	case string:
		if v == "" {
			return nil // explicit unbind
		}
		return []string{v}
	case []any:
		var keys []string
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				keys = append(keys, s)
			}
		}
		return keys
	}
	return nil
}

// LoadKeybindConfig reads the keybinding configuration file.
// Returns zero config if no file found (caller defaults to "native" style).
func LoadKeybindConfig() (KeybindConfig, error) {
	var cfg KeybindConfig
	path := findConfig("termd-tui", "keybindings.toml")
	if path == "" {
		return cfg, nil
	}
	_, err := toml.DecodeFile(path, &cfg)
	return cfg, err
}

// findConfig returns the path to a config file if it exists, checking
// XDG_CONFIG_HOME first, then ~/.config.
func findConfig(appDir, filename string) string {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg != "" {
		// XDG_CONFIG_HOME is set — only look there.
		p := filepath.Join(xdg, appDir, filename)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	// Fall back to ~/.config
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, ".config", appDir, filename)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}
