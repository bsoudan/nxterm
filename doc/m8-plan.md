# Milestone 8 Implementation Plan

5 steps.

---

## Step 1: Migrate termctl to urfave/cli v3

Update termctl from `github.com/urfave/cli/v2` to `github.com/urfave/cli/v3`.

### API changes

v3 has several breaking changes from v2:

- `cli.App` → `cli.Command` (the root app is now a Command)
- `Action: func(*cli.Context) error` → `Action: func(context.Context, *cli.Command) error`
- `c.String("flag")` → `cmd.String("flag")` (on the Command, not a separate Context)
- `c.Args()` → `cmd.Args()`
- `c.NArg()` → `cmd.NArg()`
- `c.Bool("flag")` → `cmd.Bool("flag")`
- `Aliases` on flags → just list multiple `Name` entries or use `Aliases` (check v3 API)
- `cli.StringFlag{Name: "x", Value: "default"}` → similar but struct fields may differ

### Changes

**`termctl/go.mod`**: Replace `github.com/urfave/cli/v2` with `github.com/urfave/cli/v3`.

**`termctl/main.go`**: Rewrite the app definition and all command handlers to use v3 API.
The functionality stays identical — just the API adapters change.

### Tests

All existing termctl e2e tests pass unchanged (they call the binary, not the Go API).

---

## Step 2: Config file parsing package

Create a shared `config` package that reads TOML config files from XDG paths.

### Package

**`config/config.go`** (new module at `termd/config`):

```go
// ServerConfig represents termd/server.toml
type ServerConfig struct {
    Listen  []string    `toml:"listen"`
    Debug   bool        `toml:"debug"`
    SSH     SSHConfig   `toml:"ssh"`
    Termctl TermctlConfig `toml:"termctl"`
}

type SSHConfig struct {
    HostKey        string `toml:"host-key"`
    AuthorizedKeys string `toml:"authorized-keys"`
    NoAuth         bool   `toml:"no-auth"`
}

type TermctlConfig struct {
    Connect string `toml:"connect"`
    Debug   bool   `toml:"debug"`
}

// FrontendConfig represents termd-tui/config.toml
type FrontendConfig struct {
    Connect string `toml:"connect"`
    Command string `toml:"command"`
    Debug   bool   `toml:"debug"`
}

// LoadServerConfig reads termd/server.toml from the XDG config path.
// Returns zero-value config if no file exists.
func LoadServerConfig(explicit string) (ServerConfig, error)

// LoadFrontendConfig reads termd-tui/config.toml from the XDG config path.
// If not found, falls back to the first listen address from termd/server.toml.
// Returns zero-value config if no files exist.
func LoadFrontendConfig(explicit string) (FrontendConfig, error)
```

### Config discovery logic

`LoadServerConfig(explicit)`:
1. If `explicit != ""`, read that file (error if missing).
2. Else try `$XDG_CONFIG_HOME/termd/server.toml`.
3. Else try `~/.config/termd/server.toml`.
4. If no file found, return zero config (no error).

`LoadFrontendConfig(explicit)`:
1. If `explicit != ""`, read that file (error if missing).
2. Else try `$XDG_CONFIG_HOME/termd-tui/config.toml`.
3. Else try `~/.config/termd-tui/config.toml`.
4. If no file found, try loading `ServerConfig` and use the first listen address as `Connect`.
5. If nothing found, return zero config (no error).

### TOML library

Use `github.com/BurntSushi/toml` — standard, minimal, well-maintained.

### Tests

- Unit test: parse a sample server.toml, verify all fields.
- Unit test: parse a sample frontend config.toml.
- Unit test: frontend fallback reads listen address from server.toml.
- Unit test: missing file returns zero config, no error.
- Unit test: explicit path that doesn't exist returns an error.

---

## Step 3: Wire config into the server and frontend

### Server (`server/main.go`)

- Add `--config <path>` flag.
- At startup, call `config.LoadServerConfig(configPath)`.
- For each setting: if the CLI flag was explicitly set, use it. Otherwise, use the config
  file value. Otherwise, use the built-in default.
- The `listen` config is a list; CLI `--listen` flags append to or replace it.

Resolution logic for the server:
```
listen:     CLI --listen flags if any, else config listen, else ["unix:/tmp/termd.sock"]
debug:      CLI --debug || env TERMD_DEBUG || config debug
ssh.host-key:    CLI --ssh-host-key || config ssh.host-key
ssh.auth-keys:   CLI --ssh-auth-keys || config ssh.authorized-keys
ssh.no-auth:     CLI --ssh-no-auth || config ssh.no-auth
```

### Frontend (`frontend/main.go`)

- Add `--config <path>` flag.
- At startup, call `config.LoadFrontendConfig(configPath)`.
- Resolution:
```
connect:  CLI --socket || env TERMD_SOCKET || config connect || "unix:/tmp/termd.sock"
command:  CLI --command || config command || $SHELL || bash
debug:    CLI --debug || env TERMD_DEBUG || config debug
```

### Tests

- Existing e2e tests pass (they don't use config files).
- Add a test that writes a config file to a temp dir and verifies the server reads listen
  addresses from it.

---

## Step 4: Wire config into termctl via v3 Sources

urfave/cli v3 has a `Sources` field on flags that layers value sources. Use this to feed
config file values as a source that sits below CLI flags and env vars.

### Implementation

- In termctl's `Before` hook, load `ServerConfig` and extract the `[termctl]` section.
- Create a `MapSource` (or custom `ValueSource`) that maps flag names to config values.
- Attach it to the relevant flags via `Sources`.

Alternatively, if v3's source system is too complex, just read the config in `Before` and
set flag defaults manually (same approach as server/frontend).

### Resolution for termctl:
```
socket:  CLI --socket || env TERMD_SOCKET || config termctl.connect || "unix:/tmp/termd.sock"
debug:   CLI --debug || env TERMD_DEBUG || config termctl.debug
```

### Tests

- Write a config file with `[termctl] connect = "tcp:127.0.0.1:9090"`, run termctl with
  no CLI flags, verify it connects to that address.

---

## Step 5: `--config` flag, example file, documentation

### Example files

Ship example config files in `doc/`:

**`doc/example-server.toml`**:
```toml
# termd server configuration
# Place at ~/.config/termd/server.toml

listen = ["unix:/tmp/termd.sock"]
# debug = true

[ssh]
# host-key = "~/.config/termd/host_key"
# authorized-keys = "~/.ssh/authorized_keys"
# no-auth = false

[termctl]
# connect = "unix:/tmp/termd.sock"
# debug = false
```

**`doc/example-frontend.toml`**:
```toml
# termd-tui configuration
# Place at ~/.config/termd-tui/config.toml

# connect = "unix:/tmp/termd.sock"
# command = "bash"
# debug = false
```

### Documentation

Update protocol.md or add a new `doc/configuration.md` describing:
- Config file locations and discovery
- Precedence rules (CLI > env > config > default)
- All settings with descriptions
- The frontend fallback to server.toml

---

## Dependency graph

```
Step 1 (migrate termctl to urfave/cli v3)
  → Step 2 (config parsing package)
    → Step 3 (wire into server + frontend)
    → Step 4 (wire into termctl)
    → Step 5 (examples + docs)
```

Steps 3 and 4 are independent of each other.
