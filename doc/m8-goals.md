# Milestone 8 Goals — Configuration Files

Add configuration file support to all three tools so users don't have to pass flags on every
invocation. CLI flags override config file values, which override defaults.

## Config file location

Follow XDG conventions:
- `$XDG_CONFIG_HOME/termd/config.toml` (default: `~/.config/termd/config.toml`)
- Per-tool override: `--config <path>`

A single config file serves all three tools. Each tool reads the sections relevant to it.

## Format

TOML — simple, widely supported, good for flat key-value config with sections. No external
dependency required (Go has good TOML libraries).

## Sections

```toml
[server]
listen = ["unix:/tmp/termd.sock", "tcp:127.0.0.1:9090"]
debug = false

[server.ssh]
host-key = "~/.config/termd/host_key"
authorized-keys = "~/.ssh/authorized_keys"
no-auth = false

[frontend]
connect = "unix:/tmp/termd.sock"
command = "bash"
debug = false

[termctl]
connect = "unix:/tmp/termd.sock"
debug = false
```

## Precedence

CLI flags > environment variables > config file > built-in defaults.

Each tool resolves its configuration by layering these sources. A flag set on the command line
always wins. An unset flag falls through to the config file value. An absent config key falls
through to the default.

## Config discovery

1. If `--config <path>` is given, use that file (error if missing).
2. Else check `$XDG_CONFIG_HOME/termd/config.toml`.
3. Else check `~/.config/termd/config.toml`.
4. If no config file found, use defaults (same behavior as today).

## Non-goals

- Config file generation / `init` command — users create the file manually or copy an example.
- Live reloading — config is read at startup only.
- Per-directory config — only the user-level config file is checked.
