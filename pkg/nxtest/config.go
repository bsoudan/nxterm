package nxtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WriteServerConfig creates a server.toml in the XDG config dir that
// configures a "shell" program (bash --norc) as the default.
func WriteServerConfig(env []string) error {
	xdg := XDGFromEnv(env)
	if xdg == "" {
		return nil
	}
	shell, _ := exec.LookPath("bash")
	if shell == "" {
		shell = "bash"
	}
	cfgDir := filepath.Join(xdg, "nxtermd")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	content := fmt.Sprintf("[[programs]]\nname = \"shell\"\ncmd = %q\nargs = [\"--norc\"]\n\n[sessions]\ndefault-programs = [\"shell\"]\n", shell)
	return os.WriteFile(filepath.Join(cfgDir, "server.toml"), []byte(content), 0o644)
}

// WriteServerConfigCustom creates a server.toml with the given raw TOML content.
func WriteServerConfigCustom(env []string, content string) error {
	xdg := XDGFromEnv(env)
	if xdg == "" {
		return nil
	}
	cfgDir := filepath.Join(xdg, "nxtermd")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(filepath.Join(cfgDir, "server.toml"), []byte(content), 0o644)
}

// WriteKeybindConfig creates a keybindings.toml in the XDG config dir for nxterm.
func WriteKeybindConfig(env []string, content string) error {
	xdg := XDGFromEnv(env)
	if xdg == "" {
		return nil
	}
	cfgDir := filepath.Join(xdg, "nxterm")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(filepath.Join(cfgDir, "keybindings.toml"), []byte(content), 0o644)
}

// XDGFromEnv extracts XDG_CONFIG_HOME from an environment slice.
func XDGFromEnv(env []string) string {
	for _, e := range env {
		if strings.HasPrefix(e, "XDG_CONFIG_HOME=") {
			return e[len("XDG_CONFIG_HOME="):]
		}
	}
	return ""
}

// TestEnv returns os.Environ with XDG_CONFIG_HOME set to tmpDir,
// isolating from the user's local configuration files.
func TestEnv(tmpDir string) []string {
	return append(os.Environ(), "XDG_CONFIG_HOME="+tmpDir)
}
