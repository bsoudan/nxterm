package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// CLI command actions — each sends an IPC request to the daemon.

func cmdStart(ctx context.Context, cmd *cli.Command) error {
	cols := int(cmd.Int("cols"))
	rows := int(cmd.Int("rows"))
	name := cmd.Root().String("name")
	d := newDaemon(cols, rows)
	return d.start(name)
}

func cmdScreen(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Root().String("name")
	params, _ := json.Marshal(screenParams{
		JSON: cmd.Bool("json"),
		Trim: cmd.Bool("trim"),
	})
	resp, err := ipcCall(name, &ipcRequest{Command: "screen", Params: params})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	if cmd.Bool("json") {
		fmt.Printf("%s\n", resp.Data)
		return nil
	}
	var result screenResult
	json.Unmarshal(resp.Data, &result)
	for _, line := range result.Lines {
		fmt.Println(line)
	}
	return nil
}

func cmdSend(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Root().String("name")
	input := strings.Join(cmd.Args().Slice(), " ")
	if input == "" {
		return fmt.Errorf("input required")
	}
	params, _ := json.Marshal(sendParams{
		Input:  input,
		Escape: cmd.Bool("escape"),
	})
	resp, err := ipcCall(name, &ipcRequest{Command: "send", Params: params})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func cmdWait(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Root().String("name")
	text := strings.Join(cmd.Args().Slice(), " ")
	if text == "" {
		return fmt.Errorf("text required")
	}
	params, _ := json.Marshal(waitParams{
		Text:    text,
		Timeout: cmd.Duration("timeout").String(),
		Regex:   cmd.Bool("regex"),
		Not:     cmd.Bool("not"),
	})
	resp, err := ipcCall(name, &ipcRequest{Command: "wait", Params: params})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func cmdResize(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Root().String("name")
	args := cmd.Args().Slice()
	if len(args) != 2 {
		return fmt.Errorf("usage: nxtest resize <cols> <rows>")
	}
	cols, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid cols: %w", err)
	}
	rows, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid rows: %w", err)
	}
	params, _ := json.Marshal(resizeParams{Cols: cols, Rows: rows})
	resp, err := ipcCall(name, &ipcRequest{Command: "resize", Params: params})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func cmdStatus(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Root().String("name")
	resp, err := ipcCall(name, &ipcRequest{Command: "status"})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	var result statusResult
	json.Unmarshal(resp.Data, &result)
	fmt.Printf("running: %v\ncols: %d\nrows: %d\n", result.Running, result.Cols, result.Rows)
	return nil
}

func cmdStop(ctx context.Context, cmd *cli.Command) error {
	name := cmd.Root().String("name")
	resp, err := ipcCall(name, &ipcRequest{Command: "stop"})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

// Daemon-side IPC handlers.

func (d *daemon) handleScreen(params json.RawMessage) *ipcResponse {
	var p screenParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}
	lines := d.fe.ScreenLines()
	if p.Trim {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
	}
	data, _ := json.Marshal(screenResult{Lines: lines})
	return &ipcResponse{Data: data}
}

func (d *daemon) handleSend(params json.RawMessage) *ipcResponse {
	var p sendParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ipcResponse{Error: fmt.Sprintf("bad params: %v", err)}
	}
	input := p.Input
	if p.Escape {
		input = interpretEscapes(input)
	}
	d.fe.Write([]byte(input))
	return &ipcResponse{}
}

func (d *daemon) handleWait(params json.RawMessage) *ipcResponse {
	var p waitParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ipcResponse{Error: fmt.Sprintf("bad params: %v", err)}
	}
	timeout, err := time.ParseDuration(p.Timeout)
	if err != nil {
		return &ipcResponse{Error: fmt.Sprintf("bad timeout: %v", err)}
	}

	var re *regexp.Regexp
	if p.Regex {
		re, err = regexp.Compile(p.Text)
		if err != nil {
			return &ipcResponse{Error: fmt.Sprintf("bad regex: %v", err)}
		}
	}

	check := func(lines []string) bool {
		for _, line := range lines {
			var found bool
			if re != nil {
				found = re.MatchString(line)
			} else {
				found = strings.Contains(line, p.Text)
			}
			if found {
				return !p.Not
			}
		}
		return p.Not
	}

	desc := fmt.Sprintf("screen to contain %q", p.Text)
	if p.Not {
		desc = fmt.Sprintf("screen to not contain %q", p.Text)
	}

	_, err = d.fe.WaitForScreen(check, desc, timeout)
	if err != nil {
		return &ipcResponse{Error: err.Error()}
	}
	return &ipcResponse{}
}

func (d *daemon) handleResize(params json.RawMessage) *ipcResponse {
	var p resizeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ipcResponse{Error: fmt.Sprintf("bad params: %v", err)}
	}
	if p.Cols < 1 || p.Rows < 1 {
		return &ipcResponse{Error: "cols and rows must be positive"}
	}
	d.fe.Resize(uint16(p.Cols), uint16(p.Rows))
	d.mu.Lock()
	d.cols = p.Cols
	d.rows = p.Rows
	d.mu.Unlock()
	return &ipcResponse{}
}

func (d *daemon) handleStatus() *ipcResponse {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, _ := json.Marshal(statusResult{
		Running: true,
		Cols:    d.cols,
		Rows:    d.rows,
	})
	return &ipcResponse{Data: data}
}

func (d *daemon) handleStop() *ipcResponse {
	d.stop()
	return &ipcResponse{}
}

// interpretEscapes processes backslash escape sequences in a string.
func interpretEscapes(s string) string {
	var buf strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				buf.WriteByte('\n')
			case 'r':
				buf.WriteByte('\r')
			case 't':
				buf.WriteByte('\t')
			case '\\':
				buf.WriteByte('\\')
			case 'x':
				if i+2 < len(s) {
					if b, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
						buf.WriteByte(byte(b))
						i += 2
					} else {
						buf.WriteByte('\\')
						buf.WriteByte('x')
					}
				}
			case '0':
				end := i + 1
				for end < len(s) && end < i+4 && s[end] >= '0' && s[end] <= '7' {
					end++
				}
				if end > i+1 {
					if b, err := strconv.ParseUint(s[i+1:end], 8, 8); err == nil {
						buf.WriteByte(byte(b))
						i = end - 1
					}
				} else {
					buf.WriteByte('\x00')
				}
			default:
				buf.WriteByte('\\')
				buf.WriteByte(s[i])
			}
		} else {
			buf.WriteByte(s[i])
		}
		i++
	}
	return buf.String()
}
