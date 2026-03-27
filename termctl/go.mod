module termd/termctl

go 1.25.7

replace termd/config => ../config

replace termd/frontend => ../frontend

replace termd/transport => ../transport

require (
	github.com/urfave/cli/v3 v3.8.0
	termd/config v0.0.0
	termd/frontend v0.0.0-00010101000000-000000000000
	termd/transport v0.0.0
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-runewidth v0.0.20 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)
