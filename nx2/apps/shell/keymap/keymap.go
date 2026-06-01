// Package keymap is the nx2 shell's keybinding engine: a prefix-key + chord trie
// with style presets (native/tmux/screen/zellij), always-active raw-byte bindings
// (alt+x, ctrl+fN, pgup), and virtual bindings (wheelup/wheeldown). It is ported
// from internal/tui/keybind.go with the bubbletea coupling removed: instead of
// returning tea.Cmd dispatch, NewRegistry yields plain data and a Matcher (see
// matcher.go) consumes raw input bytes to produce (command, args) results. The
// package is WASM-clean so the shell guest can link it.
package keymap

import (
	"fmt"
	"strings"
)

// Command is a named user action. Category groups it in help/config; Layer is
// informational (which nxterm layer historically handled it).
type Command struct {
	Name        string
	Category    string // "tab", "session", "main"
	Layer       string
	Description string
}

// resolvedBinding pairs a command with its arguments and display key.
type resolvedBinding struct {
	command *Command
	args    string
	key     string
}

// alwaysBinding is a raw byte pattern that triggers a command without a prefix.
type alwaysBinding struct {
	raw     []byte
	command *Command
	args    string
	key     string
	when    string // "" = always, "normal-screen" = only outside the alt screen
}

// virtualBinding is a logical key name (e.g. "wheelup") with no raw bytes,
// dispatched by application code that looks it up by name.
type virtualBinding struct {
	command *Command
	args    string
	key     string
	when    string
}

// chordNode is a trie node for matching multi-key chord sequences.
type chordNode struct {
	binding  *resolvedBinding
	children map[string]*chordNode
}

func (n *chordNode) insert(keys []string, b resolvedBinding) {
	cur := n
	for _, k := range keys {
		if cur.children == nil {
			cur.children = make(map[string]*chordNode)
		}
		child, ok := cur.children[k]
		if !ok {
			child = &chordNode{}
			cur.children[k] = child
		}
		cur = child
	}
	cur.binding = &b
}

func (n *chordNode) match(keys []string) (binding *resolvedBinding, isPrefix bool) {
	cur := n
	for _, k := range keys {
		if cur.children == nil {
			return nil, false
		}
		child, ok := cur.children[k]
		if !ok {
			return nil, false
		}
		cur = child
	}
	return cur.binding, len(cur.children) > 0
}

// Registry holds all commands and resolved bindings for one style.
type Registry struct {
	commands  []*Command
	byName    map[string]*Command
	chordRoot chordNode
	always    []alwaysBinding
	virtual   map[string]*virtualBinding
	bindings  []BindingInfo

	PrefixKey byte
	PrefixStr string
	Style     string
}

// BindingInfo describes a resolved keybinding for introspection (help/palette).
type BindingInfo struct {
	Category    string
	Key         string // raw key spec, e.g. "c", "alt+x", "S o"
	KeyDisplay  string // pretty: "ctrl+b, c" or "alt+x"
	CommandName string
	Args        string
	Description string
	Always      bool
}

// Bindings returns all resolved bindings in display order (grouped by category).
func (r *Registry) Bindings() []BindingInfo { return r.bindings }

// MatchChord checks the chord trie for the given key tokens. Returns the matched
// binding (nil if none) and whether the tokens are a prefix of a longer chord.
func (r *Registry) MatchChord(keys []string) (cmd *Command, args string, isPrefix bool) {
	b, isPrefix := r.chordRoot.match(keys)
	if b == nil {
		return nil, "", isPrefix
	}
	return b.command, b.args, isPrefix
}

// LookupVirtual returns the command bound to a virtual key (e.g. "wheelup").
func (r *Registry) LookupVirtual(name string) (cmd *Command, args, when string, ok bool) {
	vb, ok := r.virtual[name]
	if !ok {
		return nil, "", "", false
	}
	return vb.command, vb.args, vb.when, true
}

// --- Command definitions ---

var categories = []string{"main", "session", "tab"}

func allCommands() []*Command {
	return []*Command{
		{Name: "run-command", Category: "main", Layer: "main", Description: "command palette"},
		{Name: "detach", Category: "main", Layer: "main", Description: "detach"},
		{Name: "show-help", Category: "main", Layer: "main", Description: "show keybindings"},
		{Name: "show-log", Category: "main", Layer: "main", Description: "open log viewer"},
		{Name: "show-release-notes", Category: "main", Layer: "main", Description: "show release notes"},
		{Name: "open-session", Category: "session", Layer: "session-manager", Description: "create new session"},
		{Name: "close-session", Category: "session", Layer: "session-manager", Description: "kill current session"},
		{Name: "next-session", Category: "session", Layer: "session-manager", Description: "next session"},
		{Name: "prev-session", Category: "session", Layer: "session-manager", Description: "previous session"},
		{Name: "switch-session", Category: "session", Layer: "session-manager", Description: "switch session"},
		{Name: "show-status", Category: "session", Layer: "session-manager", Description: "show status"},
		{Name: "open-tab", Category: "tab", Layer: "session", Description: "open new tab"},
		{Name: "close-tab", Category: "tab", Layer: "session", Description: "close active tab"},
		{Name: "next-tab", Category: "tab", Layer: "session", Description: "next tab"},
		{Name: "prev-tab", Category: "tab", Layer: "session", Description: "previous tab"},
		{Name: "switch-tab", Category: "tab", Layer: "session", Description: "switch to tab N"},
		{Name: "scroll-up", Category: "tab", Layer: "session", Description: "enter scrollback / scroll up"},
		{Name: "scroll-down", Category: "tab", Layer: "session", Description: "scroll down (in scrollback)"},
		{Name: "send-prefix", Category: "tab", Layer: "session", Description: "send literal prefix key"},
		{Name: "enter-scrollback", Category: "tab", Layer: "session", Description: "enter scrollback mode"},
		{Name: "refresh-screen", Category: "tab", Layer: "session", Description: "refresh screen"},
	}
}

// --- Style presets ---

type binding struct {
	key         string
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
			{"1", "switch-tab", "1"}, {"2", "switch-tab", "2"}, {"3", "switch-tab", "3"},
			{"4", "switch-tab", "4"}, {"5", "switch-tab", "5"}, {"6", "switch-tab", "6"},
			{"7", "switch-tab", "7"}, {"8", "switch-tab", "8"}, {"9", "switch-tab", "9"},
			{"S o", "open-session", ""},
			{"S c", "close-session", ""},
			{"ctrl+.", "next-session", ""},
			{"ctrl+,", "prev-session", ""},
			{"w", "switch-session", ""},
			{"d", "detach", ""},
			{":", "run-command", ""},
			{"ctrl+b", "send-prefix", ""},
			{"l", "show-log", ""},
			{"?", "show-help", ""},
			{"s", "show-status", ""},
			{"n", "show-release-notes", ""},
			{"[", "enter-scrollback", ""},
			{"r", "refresh-screen", ""},
			{"pgup?normal-screen", "scroll-up", ""},
			{"pgdown?normal-screen", "scroll-down", ""},
			{"wheelup?normal-screen", "scroll-up", ""},
			{"wheeldown?normal-screen", "scroll-down", ""},
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
			{"0", "switch-tab", "0"}, {"1", "switch-tab", "1"}, {"2", "switch-tab", "2"},
			{"3", "switch-tab", "3"}, {"4", "switch-tab", "4"}, {"5", "switch-tab", "5"},
			{"6", "switch-tab", "6"}, {"7", "switch-tab", "7"}, {"8", "switch-tab", "8"},
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
			{"pgup?normal-screen", "scroll-up", ""},
			{"pgdown?normal-screen", "scroll-down", ""},
			{"wheelup?normal-screen", "scroll-up", ""},
			{"wheeldown?normal-screen", "scroll-down", ""},
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
			{"0", "switch-tab", "0"}, {"1", "switch-tab", "1"}, {"2", "switch-tab", "2"},
			{"3", "switch-tab", "3"}, {"4", "switch-tab", "4"}, {"5", "switch-tab", "5"},
			{"6", "switch-tab", "6"}, {"7", "switch-tab", "7"}, {"8", "switch-tab", "8"},
			{"9", "switch-tab", "9"},
			{"S", "open-session", ""},
			{"\"", "switch-session", ""},
			{"d", "detach", ""},
			{"ctrl+a", "send-prefix", ""},
			{"?", "show-help", ""},
			{"[", "enter-scrollback", ""},
			{"l", "show-log", ""},
			{"pgup?normal-screen", "scroll-up", ""},
			{"pgdown?normal-screen", "scroll-down", ""},
			{"wheelup?normal-screen", "scroll-up", ""},
			{"wheeldown?normal-screen", "scroll-down", ""},
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
			{"alt+1", "switch-tab", "1"}, {"alt+2", "switch-tab", "2"}, {"alt+3", "switch-tab", "3"},
			{"alt+4", "switch-tab", "4"}, {"alt+5", "switch-tab", "5"}, {"alt+6", "switch-tab", "6"},
			{"alt+7", "switch-tab", "7"}, {"alt+8", "switch-tab", "8"}, {"alt+9", "switch-tab", "9"},
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
			{"pgup?normal-screen", "scroll-up", ""},
			{"pgdown?normal-screen", "scroll-down", ""},
			{"wheelup?normal-screen", "scroll-up", ""},
			{"wheeldown?normal-screen", "scroll-down", ""},
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

var fnKeyCodes = map[string]struct {
	code string
	end  byte
}{
	"f1": {"1", 'P'}, "f2": {"1", 'Q'}, "f3": {"1", 'R'}, "f4": {"1", 'S'},
	"f5": {"15", '~'}, "f6": {"17", '~'}, "f7": {"18", '~'}, "f8": {"19", '~'},
	"f9": {"20", '~'}, "f10": {"21", '~'}, "f11": {"23", '~'}, "f12": {"24", '~'},
}

var namedKeys = map[string][]byte{
	"pgup":   {0x1b, '[', '5', '~'},
	"pgdown": {0x1b, '[', '6', '~'},
}

var virtualKeys = map[string]bool{
	"wheelup":   true,
	"wheeldown": true,
}

// parseKeyCondition splits "pgup?normal-screen" into ("pgup", "normal-screen").
func parseKeyCondition(spec string) (key, when string) {
	if i := strings.IndexByte(spec, '?'); i > 0 && i < len(spec)-1 {
		return spec[:i], spec[i+1:]
	}
	return spec, ""
}

const (
	modAlt  = "3"
	modCtrl = "5"
)

// keyToRawBytes converts an always-binding key spec to its raw terminal bytes.
func keyToRawBytes(key string) []byte {
	if raw, ok := namedKeys[key]; ok {
		return raw
	}
	if strings.HasPrefix(key, "alt+") {
		rest := key[len("alt+"):]
		if fk, ok := fnKeyCodes[rest]; ok {
			return []byte(fmt.Sprintf("\x1b[%s;%s%c", fk.code, modAlt, fk.end))
		}
		if len(rest) == 1 {
			return []byte{0x1b, rest[0]}
		}
		return nil
	}
	if strings.HasPrefix(key, "ctrl+") {
		rest := key[len("ctrl+"):]
		if fk, ok := fnKeyCodes[rest]; ok {
			return []byte(fmt.Sprintf("\x1b[%s;%s%c", fk.code, modCtrl, fk.end))
		}
		if len(rest) == 1 {
			c := rest[0]
			if c >= '!' && c <= '~' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') {
				return []byte(fmt.Sprintf("\x1b[%d;5u", c))
			}
		}
		return nil
	}
	return nil
}

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

// isAlwaysKey reports whether a key spec is an always-active (raw-byte) binding.
func isAlwaysKey(key string) bool {
	if _, ok := namedKeys[key]; ok {
		return true
	}
	if strings.HasPrefix(key, "alt+") {
		return true
	}
	if strings.HasPrefix(key, "ctrl+") {
		rest := key[len("ctrl+"):]
		if _, ok := fnKeyCodes[rest]; ok {
			return true
		}
		if len(rest) == 1 {
			c := rest[0]
			return c >= '!' && c <= '~' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z')
		}
	}
	return false
}

func parseCommandInvocation(s string) (name, args string) {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func commandInvocation(name, args string) string {
	if args == "" {
		return name
	}
	return name + " " + args
}

// --- Registry builder ---

// NewRegistry builds a Registry from a style preset and optional overrides
// (invocation -> key specs). prefix overrides the preset's prefix key.
func NewRegistry(style, prefix string, overrides map[string][]string) *Registry {
	resolvedStyle := style
	if resolvedStyle == "" {
		resolvedStyle = "native"
	}
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

	bindings := make([]binding, len(preset.bindings))
	copy(bindings, preset.bindings)
	for i := range bindings {
		if bindings[i].commandName == "send-prefix" {
			bindings[i].key = prefixStr
		}
	}

	if len(overrides) > 0 {
		overridden := make(map[string]bool, len(overrides))
		for inv := range overrides {
			overridden[inv] = true
		}
		filtered := bindings[:0]
		for _, b := range bindings {
			if !overridden[commandInvocation(b.commandName, b.args)] {
				filtered = append(filtered, b)
			}
		}
		bindings = filtered
		for inv, keys := range overrides {
			name, args := parseCommandInvocation(inv)
			if _, ok := byName[name]; !ok {
				continue
			}
			for _, key := range keys {
				bindings = append(bindings, binding{key: key, commandName: name, args: args})
			}
		}
	}

	r := &Registry{
		commands:  cmds,
		byName:    byName,
		PrefixKey: prefixByte,
		PrefixStr: prefixStr,
		Style:     resolvedStyle,
	}

	type catBinding struct {
		b       binding
		cmd     *Command
		keyBase string
	}
	catBindings := make(map[string][]catBinding)
	for _, b := range bindings {
		cmd := byName[b.commandName]
		if cmd == nil {
			continue
		}
		key, when := parseKeyCondition(b.key)
		switch {
		case virtualKeys[key]:
			if r.virtual == nil {
				r.virtual = make(map[string]*virtualBinding)
			}
			r.virtual[key] = &virtualBinding{command: cmd, args: b.args, key: key, when: when}
		case isAlwaysKey(key):
			raw := keyToRawBytes(key)
			if raw == nil {
				continue
			}
			r.always = append(r.always, alwaysBinding{raw: raw, command: cmd, args: b.args, key: key, when: when})
		default:
			r.chordRoot.insert(strings.Fields(key), resolvedBinding{command: cmd, args: b.args, key: key})
		}
		catBindings[cmd.Category] = append(catBindings[cmd.Category], catBinding{b: b, cmd: cmd, keyBase: key})
	}

	for _, cat := range categories {
		for _, cb := range catBindings[cat] {
			isVirtual := virtualKeys[cb.keyBase]
			isAlways := isAlwaysKey(cb.keyBase)
			keyDisp := cb.keyBase
			if !isAlways && !isVirtual {
				keyDisp = prefixStr + ", " + strings.ReplaceAll(cb.keyBase, " ", ", ")
			}
			r.bindings = append(r.bindings, BindingInfo{
				Category:    cat,
				Key:         cb.keyBase,
				KeyDisplay:  keyDisp,
				CommandName: cb.cmd.Name,
				Args:        cb.b.args,
				Description: cb.cmd.Description,
				Always:      isAlways || isVirtual,
			})
		}
	}
	return r
}
