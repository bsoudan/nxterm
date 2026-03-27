# Milestone 8 Goals — Configuration Files

Add configuration file support to all three tools so users don't have to pass flags on every
invocation. CLI flags override config file values, which override defaults.

## Config file locations

Follow XDG conventions with separate files for the server-side and frontend:
- Server + termctl: `$XDG_CONFIG_HOME/termd/server.toml` (default: `~/.config/termd/server.toml`)
- Frontend: `$XDG_CONFIG_HOME/termd-tui/config.toml` (default: `~/.config/termd-tui/config.toml`)
- Per-tool override: `--config <path>`

The server and termctl share a config file since they're administered together (same listen
addresses, SSH settings). The frontend has its own config since it may run on a different
machine.

## Format

TOML — simple, widely supported, good for flat key-value config with sections. No external
dependency required (Go has good TOML libraries).

## File contents

### termd/server.toml

```toml
# Server settings
listen = ["unix:/tmp/termd.sock", "tcp:127.0.0.1:9090"]
debug = false

[ssh]
host-key = "~/.config/termd/host_key"
authorized-keys = "~/.ssh/authorized_keys"
no-auth = false

# termctl defaults (shares the server config file)
[termctl]
connect = "unix:/tmp/termd.sock"
debug = false
```

### termd-tui/config.toml

```toml
connect = "unix:/tmp/termd.sock"
command = "bash"
debug = false
```

## Precedence

CLI flags > environment variables > config file > built-in defaults.

Each tool resolves its configuration by layering these sources. A flag set on the command line
always wins. An unset flag falls through to the config file value. An absent config key falls
through to the default.

## Config discovery

For server and termctl:
1. If `--config <path>` is given, use that file (error if missing).
2. Else check `$XDG_CONFIG_HOME/termd/server.toml`.
3. Else check `~/.config/termd/server.toml`.
4. If no config file found, use defaults (same behavior as today).

For the frontend:
1. If `--config <path>` is given, use that file (error if missing).
2. Else check `$XDG_CONFIG_HOME/termd-tui/config.toml`.
3. Else check `~/.config/termd-tui/config.toml`.
4. If no config file found, use defaults (same behavior as today).

## Non-goals

- Config file generation / `init` command — users create the file manually or copy an example.
- Live reloading — config is read at startup only.
- Per-directory config — only the user-level config file is checked.
