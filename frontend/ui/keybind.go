package ui

import (
	"log/slog"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Command is a named user action with a description, category, and a factory
// that produces the tea.Cmd to dispatch. Commands that accept arguments
// receive them as a string (e.g., "3" for switch-tab 3).
type Command struct {
	Name        string
	Category    string
	Description string
	CmdFn       func(args string) tea.Cmd
}

// BindingType distinguishes raw-byte interception from prefix+key chords.
type BindingType int

const (
	BindChord  BindingType = iota // prefix key then key (processed as tea.KeyPressMsg)
	BindAlways                    // intercepted from raw bytes (e.g., alt+key)
)

// resolvedBinding pairs a command with its arguments and display key.
type resolvedBinding struct {
	command *Command
	args    string
	key     string // display key for help
}

// alwaysBinding is a raw byte pattern that triggers a command.
type alwaysBinding struct {
	raw     []byte
	command *Command
	args    string
	key     string // display key for help
}

// Registry holds all commands and resolved bindings.
type Registry struct {
	commands []*Command                  // ordered for help display
	byName   map[string]*Command        // name -> command
	chords   map[string]resolvedBinding // chord key string -> binding
	always   []alwaysBinding            // raw byte patterns to scan for
	// display items grouped by category for help overlay
	displayOrder []displayEntry

	PrefixKey byte   // 0x02 for ctrl+b, 0x01 for ctrl+a, etc.
	PrefixStr string // "ctrl+b" for display
}

type displayEntry struct {
	keyDisplay  string
	description string
	cmdFn       func() tea.Cmd // nil for display-only entries (always-bindings, headers)
	chordKey    string         // chord key for shortcut matching in help, "" for always
	isHeader    bool           // category header row
}

// Dispatch looks up a chord key and returns the command's tea.Cmd, or nil.
func (r *Registry) Dispatch(key string) tea.Cmd {
	if b, ok := r.chords[key]; ok {
		return b.command.CmdFn(b.args)
	}
	return nil
}

// DisplayEntries returns the help display items including category headers.
func (r *Registry) DisplayEntries() []displayEntry {
	return r.displayOrder
}

// --- Command definitions ---

func cmdMsg(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// categories defines the display order of categories in the help overlay.
var categories = []string{"tab", "session", "general"}

func allCommands() []*Command {
	return []*Command{
		// Tab management
		{Name: "open-tab", Category: "tab", Description: "open new tab", CmdFn: func(string) tea.Cmd { return cmdMsg(SpawnRegionMsg{}) }},
		{Name: "close-tab", Category: "tab", Description: "close active tab", CmdFn: func(string) tea.Cmd { return cmdMsg(CloseTabMsg{}) }},
		{Name: "next-tab", Category: "tab", Description: "next tab", CmdFn: func(string) tea.Cmd { return cmdMsg(NextTabMsg{}) }},
		{Name: "prev-tab", Category: "tab", Description: "previous tab", CmdFn: func(string) tea.Cmd { return cmdMsg(PrevTabMsg{}) }},
		{Name: "switch-tab", Category: "tab", Description: "switch to tab N", CmdFn: func(args string) tea.Cmd {
			if args == "" {
				return cmdMsg(SpawnRegionMsg{})
			}
			idx, err := strconv.Atoi(args)
			if err != nil || idx < 0 {
				return nil
			}
			return cmdMsg(SwitchTabMsg{Index: idx - 1})
		}},
		// Session management
		{Name: "open-session", Category: "session", Description: "create new session", CmdFn: func(string) tea.Cmd { return cmdMsg(NewSessionMsg{}) }},
		{Name: "close-session", Category: "session", Description: "kill current session", CmdFn: func(string) tea.Cmd { return cmdMsg(KillSessionMsg{}) }},
		{Name: "next-session", Category: "session", Description: "next session", CmdFn: func(string) tea.Cmd { return cmdMsg(NextSessionMsg{}) }},
		{Name: "prev-session", Category: "session", Description: "previous session", CmdFn: func(string) tea.Cmd { return cmdMsg(PrevSessionMsg{}) }},
		{Name: "switch-session", Category: "session", Description: "switch session", CmdFn: func(args string) tea.Cmd {
			if args == "" {
				return cmdMsg(OpenOverlayMsg{Name: "sessions"})
			}
			idx, err := strconv.Atoi(args)
			if err != nil || idx < 0 {
				return nil
			}
			return cmdMsg(SwitchSessionMsg{Index: idx - 1})
		}},
		// General
		{Name: "detach", Category: "general", Description: "detach from all sessions", CmdFn: func(string) tea.Cmd { return cmdMsg(DetachRequestMsg{}) }},
		{Name: "send-prefix", Category: "general", Description: "send literal prefix key", CmdFn: func(string) tea.Cmd { return cmdMsg(SendLiteralPrefixMsg{}) }},
		{Name: "show-log", Category: "general", Description: "open log viewer", CmdFn: func(string) tea.Cmd { return cmdMsg(OpenOverlayMsg{Name: "logviewer"}) }},
		{Name: "show-help", Category: "general", Description: "show keybindings", CmdFn: func(string) tea.Cmd { return cmdMsg(OpenOverlayMsg{Name: "help"}) }},
		{Name: "show-status", Category: "general", Description: "show status dialog", CmdFn: func(string) tea.Cmd { return cmdMsg(OpenOverlayMsg{Name: "status"}) }},
		{Name: "show-release-notes", Category: "general", Description: "show release notes", CmdFn: func(string) tea.Cmd { return cmdMsg(OpenOverlayMsg{Name: "release notes"}) }},
		{Name: "enter-scrollback", Category: "general", Description: "enter scrollback mode", CmdFn: func(string) tea.Cmd { return cmdMsg(EnterScrollbackMsg{}) }},
		{Name: "refresh-screen", Category: "general", Description: "refresh terminal screen", CmdFn: func(string) tea.Cmd { return cmdMsg(RefreshScreenMsg{}) }},
	}
}

// --- Style presets ---

// binding is a compact representation used in preset definitions.
type binding struct {
	key         string // chord: key after prefix; always: "alt+X"
	commandName string
	args        string
}

type stylePreset struct {
	prefixStr string
	bindings  []binding
}

func nativePreset() stylePreset {
	return stylePreset{
		prefixStr: "ctrl+b",
		bindings: []binding{
			{"c", "open-tab", ""},
			{"x", "close-tab", ""},
			{"alt+.", "next-tab", ""},
			{"alt+,", "prev-tab", ""},
			{"1", "switch-tab", "1"},
			{"2", "switch-tab", "2"},
			{"3", "switch-tab", "3"},
			{"4", "switch-tab", "4"},
			{"5", "switch-tab", "5"},
			{"6", "switch-tab", "6"},
			{"7", "switch-tab", "7"},
			{"8", "switch-tab", "8"},
			{"9", "switch-tab", "9"},
			{"S", "open-session", ""},
			{"X", "close-session", ""},
			{"w", "switch-session", ""},
			{"d", "detach", ""},
			{"ctrl+b", "send-prefix", ""},
			{"l", "show-log", ""},
			{"?", "show-help", ""},
			{"s", "show-status", ""},
			{"n", "show-release-notes", ""},
			{"[", "enter-scrollback", ""},
			{"r", "refresh-screen", ""},
		},
	}
}

func tmuxPreset() stylePreset {
	return stylePreset{
		prefixStr: "ctrl+b",
		bindings: []binding{
			{"c", "open-tab", ""},
			{"&", "close-tab", ""},
			{"n", "next-tab", ""},
			{"p", "prev-tab", ""},
			{"0", "switch-tab", "0"},
			{"1", "switch-tab", "1"},
			{"2", "switch-tab", "2"},
			{"3", "switch-tab", "3"},
			{"4", "switch-tab", "4"},
			{"5", "switch-tab", "5"},
			{"6", "switch-tab", "6"},
			{"7", "switch-tab", "7"},
			{"8", "switch-tab", "8"},
			{"9", "switch-tab", "9"},
			{"$", "open-session", ""},
			{"s", "switch-session", ""},
			{")", "next-session", ""},
			{"(", "prev-session", ""},
			{"d", "detach", ""},
			{"ctrl+b", "send-prefix", ""},
			{"?", "show-help", ""},
			{"[", "enter-scrollback", ""},
			{"l", "show-log", ""},
			{"r", "refresh-screen", ""},
		},
	}
}

func screenPreset() stylePreset {
	return stylePreset{
		prefixStr: "ctrl+a",
		bindings: []binding{
			{"c", "open-tab", ""},
			{"k", "close-tab", ""},
			{"n", "next-tab", ""},
			{"p", "prev-tab", ""},
			{"0", "switch-tab", "0"},
			{"1", "switch-tab", "1"},
			{"2", "switch-tab", "2"},
			{"3", "switch-tab", "3"},
			{"4", "switch-tab", "4"},
			{"5", "switch-tab", "5"},
			{"6", "switch-tab", "6"},
			{"7", "switch-tab", "7"},
			{"8", "switch-tab", "8"},
			{"9", "switch-tab", "9"},
			{"S", "open-session", ""},
			{"\"", "switch-session", ""},
			{"d", "detach", ""},
			{"ctrl+a", "send-prefix", ""},
			{"?", "show-help", ""},
			{"[", "enter-scrollback", ""},
			{"l", "show-log", ""},
		},
	}
}

func zellijPreset() stylePreset {
	return stylePreset{
		prefixStr: "ctrl+b",
		bindings: []binding{
			{"alt+n", "open-tab", ""},
			{"alt+x", "close-tab", ""},
			{"alt+,", "prev-tab", ""},
			{"alt+.", "next-tab", ""},
			{"alt+1", "switch-tab", "1"},
			{"alt+2", "switch-tab", "2"},
			{"alt+3", "switch-tab", "3"},
			{"alt+4", "switch-tab", "4"},
			{"alt+5", "switch-tab", "5"},
			{"alt+6", "switch-tab", "6"},
			{"alt+7", "switch-tab", "7"},
			{"alt+8", "switch-tab", "8"},
			{"alt+9", "switch-tab", "9"},
			{"S", "open-session", ""},
			{"X", "close-session", ""},
			{"w", "switch-session", ""},
			{"alt+d", "detach", ""},
			{"alt+h", "show-help", ""},
			{"alt+e", "enter-scrollback", ""},
			{"ctrl+b", "send-prefix", ""},
			{"s", "show-status", ""},
			{"l", "show-log", ""},
			{"r", "refresh-screen", ""},
		},
	}
}

func getPreset(style string) stylePreset {
	switch style {
	case "tmux":
		return tmuxPreset()
	case "screen":
		return screenPreset()
	case "zellij":
		return zellijPreset()
	default:
		return nativePreset()
	}
}

// --- Key parsing ---

// keyToRawBytes converts a key spec like "alt+," to raw terminal bytes.
func keyToRawBytes(key string) []byte {
	if !strings.HasPrefix(key, "alt+") {
		return nil
	}
	ch := key[len("alt+"):]
	if len(ch) != 1 {
		return nil
	}
	return []byte{0x1b, ch[0]}
}

// prefixKeyToByte converts "ctrl+X" to the corresponding control byte.
func prefixKeyToByte(key string) byte {
	if !strings.HasPrefix(key, "ctrl+") {
		return 0x02
	}
	ch := key[len("ctrl+"):]
	if len(ch) != 1 {
		return 0x02
	}
	c := ch[0]
	if c >= 'a' && c <= 'z' {
		return c - 'a' + 1
	}
	if c >= 'A' && c <= 'Z' {
		return c - 'A' + 1
	}
	return 0x02
}

func isAlwaysKey(key string) bool {
	return strings.HasPrefix(key, "alt+")
}

// parseCommandInvocation splits "switch-tab 3" into ("switch-tab", "3").
func parseCommandInvocation(s string) (name, args string) {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// commandInvocation joins a command name and args back into "switch-tab 3".
func commandInvocation(name, args string) string {
	if args == "" {
		return name
	}
	return name + " " + args
}

// --- Registry builder ---

// NewRegistry builds a Registry from a style preset and optional overrides.
// style is "native", "tmux", "screen", or "zellij".
// prefix overrides the preset's prefix key (e.g., "ctrl+a"). Empty uses preset default.
// overrides maps command-invocation -> key-specs. An empty slice unbinds the command.
// A nil map means no overrides.
func NewRegistry(style, prefix string, overrides map[string][]string) *Registry {
	preset := getPreset(style)

	prefixStr := preset.prefixStr
	if prefix != "" {
		prefixStr = prefix
	}
	prefixByte := prefixKeyToByte(prefixStr)

	cmds := allCommands()
	byName := make(map[string]*Command, len(cmds))
	for _, c := range cmds {
		byName[c.Name] = c
	}

	// Start with preset bindings.
	bindings := make([]binding, len(preset.bindings))
	copy(bindings, preset.bindings)

	// Update send-prefix chord key to match the current prefix.
	for i := range bindings {
		if bindings[i].commandName == "send-prefix" {
			bindings[i].key = prefixStr
		}
	}

	// Apply overrides: for each overridden command, remove ALL its preset
	// bindings, then add the new ones. Empty key list = unbind entirely.
	if len(overrides) > 0 {
		overriddenCmds := make(map[string]bool, len(overrides))
		for invocation := range overrides {
			overriddenCmds[invocation] = true
		}
		// Remove preset bindings for overridden commands.
		filtered := bindings[:0]
		for _, b := range bindings {
			inv := commandInvocation(b.commandName, b.args)
			if !overriddenCmds[inv] {
				filtered = append(filtered, b)
			}
		}
		bindings = filtered
		// Add override bindings.
		for invocation, keys := range overrides {
			cmdName, args := parseCommandInvocation(invocation)
			if _, ok := byName[cmdName]; !ok {
				slog.Warn("keybind: unknown command in override", "command", cmdName)
				continue
			}
			for _, key := range keys {
				bindings = append(bindings, binding{key: key, commandName: cmdName, args: args})
			}
		}
	}

	// Resolve bindings into chord and always maps, grouped by category.
	chords := make(map[string]resolvedBinding)
	var always []alwaysBinding

	// Group bindings by category for display.
	type catBinding struct {
		b   binding
		cmd *Command
	}
	catBindings := make(map[string][]catBinding)
	for _, b := range bindings {
		cmd := byName[b.commandName]
		if cmd == nil {
			continue
		}
		if isAlwaysKey(b.key) {
			raw := keyToRawBytes(b.key)
			if raw == nil {
				slog.Warn("keybind: cannot parse always-key", "key", b.key)
				continue
			}
			always = append(always, alwaysBinding{raw: raw, command: cmd, args: b.args, key: b.key})
		} else {
			chords[b.key] = resolvedBinding{command: cmd, args: b.args, key: b.key}
		}
		catBindings[cmd.Category] = append(catBindings[cmd.Category], catBinding{b: b, cmd: cmd})
	}

	// Build display entries with category headers.
	var display []displayEntry
	for _, cat := range categories {
		entries := catBindings[cat]
		if len(entries) == 0 {
			continue
		}
		display = append(display, displayEntry{
			keyDisplay: cat,
			isHeader:   true,
		})
		for _, cb := range entries {
			desc := cb.cmd.Description
			if cb.b.args != "" {
				desc += " " + cb.b.args
			}
			keyDisp := cb.b.key
			if !isAlwaysKey(cb.b.key) {
				keyDisp = prefixStr + " " + cb.b.key
			}
			de := displayEntry{
				keyDisplay:  keyDisp,
				description: desc,
			}
			if !isAlwaysKey(cb.b.key) {
				de.cmdFn = func() tea.Cmd { return cb.cmd.CmdFn(cb.b.args) }
				de.chordKey = cb.b.key
			}
			display = append(display, de)
		}
	}

	return &Registry{
		commands:     cmds,
		byName:       byName,
		chords:       chords,
		always:       always,
		displayOrder: display,
		PrefixKey:    prefixByte,
		PrefixStr:    prefixStr,
	}
}
