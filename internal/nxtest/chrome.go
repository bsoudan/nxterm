package nxtest

import (
	"fmt"
	"regexp"
)

// Chrome is the dual-backend tab surface a tab test body drives: the TUI (prefix
// actions + tab-bar parse) and the WinUI GUI (WinAppDriver clicks + hook). A
// body written against Chrome runs against either backend. GuiWinApp implements
// it directly; tuiChrome implements it for the PTY frontend.
type Chrome interface {
	// Tabs returns the tabs in order. For the TUI, RegionID is empty (the tab
	// bar doesn't carry ids); count and Active are populated for both backends.
	Tabs() []TabInfo
	// ActiveTabIndex returns the index of the active tab, or -1.
	ActiveTabIndex() int
	// NewTab opens a tab (TUI: prefix-c; GUI: the "+" button).
	NewTab() error
	// SwitchToTab activates the tab at index (TUI: prefix-N; GUI: click).
	SwitchToTab(index int) error
	// CloseTab closes the tab at index (TUI: switch then prefix-x; GUI: ✕).
	CloseTab(index int) error
}

// GuiWinApp is the GUI Chrome backend (WinAppDriver clicks + hook).
var _ Chrome = (*GuiWinApp)(nil)

// tuiChrome drives the TUI's tab chrome via prefix keybindings and reads tab
// state by parsing the tab-bar row (row 0). Construct with NewTuiChrome.
type tuiChrome struct {
	t      *T
	prefix byte // command prefix (ctrl+b = 0x02)
}

// NewTuiChrome returns a Chrome backed by the TUI frontend wrapped by t, using
// the default ctrl+b prefix.
func NewTuiChrome(t *T) Chrome { return &tuiChrome{t: t, prefix: 0x02} }

func (c *tuiChrome) NewTab() error {
	c.t.Write([]byte{c.prefix, 'c'})
	return nil
}

func (c *tuiChrome) SwitchToTab(index int) error {
	if index < 0 || index > 8 {
		return fmt.Errorf("tui SwitchToTab index %d out of range (prefix-N is 1..9)", index)
	}
	c.t.Write([]byte{c.prefix, byte('1' + index)})
	return nil
}

func (c *tuiChrome) CloseTab(index int) error {
	if err := c.SwitchToTab(index); err != nil {
		return err
	}
	c.t.Write([]byte{c.prefix, 'x'})
	return nil
}

func (c *tuiChrome) Tabs() []TabInfo { return parseTuiTabBar(c.t.ScreenLine(0)) }

func (c *tuiChrome) ActiveTabIndex() int {
	for i, t := range c.Tabs() {
		if t.Active {
			return i
		}
	}
	return -1
}

// tabTokenRE matches a tab label on the TUI tab bar: "[N]" for the active tab,
// "<N>" for an inactive one (the title follows the inactive form). It
// deliberately does not match the scrollback "[N/M]" form (it has a slash).
var tabTokenRE = regexp.MustCompile(`([\[<])(\d+)[\]>]`)

// parseTuiTabBar extracts tab count + active flag from the tab-bar row.
func parseTuiTabBar(row string) []TabInfo {
	matches := tabTokenRE.FindAllStringSubmatch(row, -1)
	tabs := make([]TabInfo, 0, len(matches))
	for _, m := range matches {
		tabs = append(tabs, TabInfo{Active: m[1] == "["})
	}
	return tabs
}
