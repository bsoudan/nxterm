# internal/config

TOML configuration loading with source tracking for both server and frontend.

## Key Types

- `ServerConfig` — `~/.config/nxtermd/server.toml`: listen addresses, programs, SSH, sessions, discovery, upgrade settings
- `FrontendConfig` — `~/.config/nxterm/config.toml`: connect address, debug, trace, status bar margin
- `ProgramConfig` — named programs that can be spawned (name, cmd, args, env)
- `Source` / `SourceKind` — tracks where each config value came from (file, flag, env, default, inferred, arg)

## API

- `LoadServerConfig()` / `LoadFrontendConfig()` — read from XDG paths or explicit file
- `PrintConfig()` — formats config with source attribution for `--show-config`
- `KeyLocations()` — parses TOML to map keys to line numbers
