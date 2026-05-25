package nxtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WinAppDriver is a minimal client for the WinAppDriver UI-automation server
// (legacy JSON-Wire protocol), reached on the Linux host through a QEMU
// hostfwd. It covers just what GUI chrome tests need: launch the app as a
// session, find elements by AutomationId, and click them. The rendered grid is
// still read over the test hook; this only drives the XAML chrome (tabs,
// buttons) that the opaque Win2D canvas can't expose.
type WinAppDriver struct {
	base   string
	sid    string
	client *http.Client
}

// DialWinAppDriver returns a client for addr (e.g. "127.0.0.1:4723").
func DialWinAppDriver(addr string) *WinAppDriver {
	return &WinAppDriver{
		base:   "http://" + addr,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewSession launches appPath (with space-separated appArgs) as a new session.
// WinAppDriver creates the window asynchronously and is cold-start flaky, so
// session creation is retried a few times.
func (w *WinAppDriver) NewSession(appPath, appArgs string) error {
	body := map[string]any{
		"desiredCapabilities": map[string]any{
			"app":          appPath,
			"appArguments": appArgs,
			"deviceName":   "WindowsPC",
			"platformName": "Windows",
		},
	}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		var resp struct {
			SessionID string `json:"sessionId"`
			Value     struct {
				SessionID string `json:"sessionId"`
			} `json:"value"`
		}
		if err := w.do("POST", "/session", body, &resp); err != nil {
			lastErr = err
			time.Sleep(time.Second)
			continue
		}
		w.sid = resp.SessionID
		if w.sid == "" {
			w.sid = resp.Value.SessionID
		}
		if w.sid != "" {
			return nil
		}
		lastErr = fmt.Errorf("no session id in response")
		time.Sleep(time.Second)
	}
	return fmt.Errorf("winappdriver new session: %w", lastErr)
}

// Close ends the session, which closes the launched app.
func (w *WinAppDriver) Close() {
	if w.sid != "" {
		_ = w.do("DELETE", "/session/"+w.sid, nil, nil)
		w.sid = ""
	}
}

// FindByAID returns the element ids of all elements with the given
// AutomationId.
func (w *WinAppDriver) FindByAID(aid string) ([]string, error) {
	var resp struct {
		Value []map[string]string `json:"value"`
	}
	body := map[string]any{"using": "accessibility id", "value": aid}
	if err := w.do("POST", "/session/"+w.sid+"/elements", body, &resp); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(resp.Value))
	for _, m := range resp.Value {
		if id := elementID(m); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// FindInByAID returns the first descendant of parent with the given
// AutomationId.
func (w *WinAppDriver) FindInByAID(parent, aid string) (string, error) {
	var resp struct {
		Value map[string]string `json:"value"`
	}
	body := map[string]any{"using": "accessibility id", "value": aid}
	if err := w.do("POST", "/session/"+w.sid+"/element/"+parent+"/element", body, &resp); err != nil {
		return "", err
	}
	id := elementID(resp.Value)
	if id == "" {
		return "", fmt.Errorf("element %q not found in %q", aid, parent)
	}
	return id, nil
}

// Click clicks the element.
func (w *WinAppDriver) Click(elementID string) error {
	return w.do("POST", "/session/"+w.sid+"/element/"+elementID+"/click", map[string]any{}, nil)
}

// SendKeys types text into the element (legacy /value endpoint). The element is
// clicked first so it has keyboard focus. Text is sent as a single-element
// value array, which WinAppDriver types verbatim.
func (w *WinAppDriver) SendKeys(elementID, text string) error {
	if err := w.Click(elementID); err != nil {
		return err
	}
	return w.do("POST", "/session/"+w.sid+"/element/"+elementID+"/value",
		map[string]any{"value": []string{text}}, nil)
}

// MoveTo moves the mouse pointer by (x, y) relative to its current position
// (legacy /moveto with no element). MoveToElement first to establish a known
// anchor, then MoveTo to step into the canvas. PointerDown/PointerUp press and
// release the left button at the current position. Together these drive a real
// drag through the OS input stack — which, unlike a QMP drag, leaves the app in
// the foreground so a following synthetic key event (e.g. Ctrl+Shift+C) reaches
// the canvas.
func (w *WinAppDriver) MoveTo(x, y int) error {
	return w.do("POST", "/session/"+w.sid+"/moveto", map[string]any{"xoffset": x, "yoffset": y}, nil)
}

// MoveToElement moves the pointer to (xoffset, yoffset) relative to the
// element's top-left corner, giving an absolute anchor (the Win2D canvas has no
// UIA peer to target directly, so tests anchor off a findable element such as
// the status bar and step from there with MoveTo).
func (w *WinAppDriver) MoveToElement(elementID string, xoffset, yoffset int) error {
	return w.do("POST", "/session/"+w.sid+"/moveto",
		map[string]any{"element": elementID, "xoffset": xoffset, "yoffset": yoffset}, nil)
}

// PointerDown presses the left mouse button at the current pointer position.
func (w *WinAppDriver) PointerDown() error {
	return w.do("POST", "/session/"+w.sid+"/buttondown", map[string]any{"button": 0}, nil)
}

// PointerUp releases the left mouse button at the current pointer position.
func (w *WinAppDriver) PointerUp() error {
	return w.do("POST", "/session/"+w.sid+"/buttonup", map[string]any{"button": 0}, nil)
}

// Keys sends keystrokes to the focused element of the session (legacy /keys).
// Used for chords like Ctrl+Shift+C after a pointer drag; WinAppDriver maps
// WebDriver key codepoints (e.g.  Ctrl,  Shift) with the usual
// "press the listed modifiers, send the final char, release" semantics.
func (w *WinAppDriver) Keys(text string) error {
	return w.do("POST", "/session/"+w.sid+"/keys",
		map[string]any{"value": []string{text}}, nil)
}

// ElementRect returns the element's absolute screen rectangle, via the legacy
// /location and /size endpoints.
func (w *WinAppDriver) ElementRect(elementID string) (x, y, width, height int, err error) {
	var loc struct {
		Value struct{ X, Y int } `json:"value"`
	}
	if err = w.do("GET", "/session/"+w.sid+"/element/"+elementID+"/location", nil, &loc); err != nil {
		return
	}
	var size struct {
		Value struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"value"`
	}
	if err = w.do("GET", "/session/"+w.sid+"/element/"+elementID+"/size", nil, &size); err != nil {
		return
	}
	return loc.Value.X, loc.Value.Y, size.Value.Width, size.Value.Height, nil
}

// elementID pulls the element id out of a find-response map, tolerating both
// the legacy "ELEMENT" key and the W3C element key.
func elementID(m map[string]string) string {
	if id, ok := m["ELEMENT"]; ok {
		return id
	}
	for _, v := range m {
		if v != "" {
			return v
		}
	}
	return ""
}

func (w *WinAppDriver) do(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, w.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, string(data))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s %s: decode %q: %w", method, path, string(data), err)
		}
	}
	return nil
}
