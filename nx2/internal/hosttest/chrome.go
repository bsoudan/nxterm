package hosttest

import (
	"fmt"

	"nxtermd/internal/nxtest"
)

// shellChrome drives the nx2 shell guest's tab chrome via prefix keybindings
// (the native preset: ctrl+b c / 1..9 / x) and reads tab state by parsing the
// tab-bar row's cells — labels are " N " runs with the active tab in reverse
// video (see the guest's drawTabBar). The nx2 counterpart of nxtest's
// tuiChrome and GuiWinApp backends.
type shellChrome struct {
	nxt    *nxtest.T
	prefix byte
}

// NewShellChrome returns a nxtest.Chrome backed by the nx2 shell guest wrapped
// by nxt, using the default ctrl+b prefix.
func NewShellChrome(nxt *nxtest.T) nxtest.Chrome { return &shellChrome{nxt: nxt, prefix: 0x02} }

func (c *shellChrome) NewTab() error {
	c.nxt.Write([]byte{c.prefix, 'c'})
	return nil
}

func (c *shellChrome) SwitchToTab(index int) error {
	if index < 0 || index > 8 {
		return fmt.Errorf("shell SwitchToTab index %d out of range (prefix-N is 1..9)", index)
	}
	c.nxt.Write([]byte{c.prefix, byte('1' + index)})
	return nil
}

func (c *shellChrome) CloseTab(index int) error {
	if err := c.SwitchToTab(index); err != nil {
		return err
	}
	c.nxt.Write([]byte{c.prefix, 'x'})
	return nil
}

// Tabs parses the tab-bar row: each run of digit cells is one tab label, and
// the active tab's cells carry the reverse attribute.
func (c *shellChrome) Tabs() []nxtest.TabInfo {
	cells := c.nxt.ScreenCells()
	if len(cells) == 0 {
		return nil
	}
	var tabs []nxtest.TabInfo
	inDigits, active := false, false
	for _, cell := range cells[0] {
		isDigit := len(cell.Data) == 1 && cell.Data[0] >= '0' && cell.Data[0] <= '9'
		switch {
		case isDigit && !inDigits:
			inDigits, active = true, cell.Attr.Reverse
		case isDigit:
			active = active || cell.Attr.Reverse
		case inDigits:
			tabs = append(tabs, nxtest.TabInfo{Active: active})
			inDigits = false
		}
	}
	if inDigits {
		tabs = append(tabs, nxtest.TabInfo{Active: active})
	}
	return tabs
}

func (c *shellChrome) ActiveTabIndex() int {
	for i, t := range c.Tabs() {
		if t.Active {
			return i
		}
	}
	return -1
}
