package main

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
)

// IPC request/response types for the nxtest Unix socket protocol.
// Each IPC connection handles exactly one request/response pair.

type ipcRequest struct {
	Command string          `json:"command"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type ipcResponse struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Command-specific parameter types.

type screenParams struct {
	JSON bool `json:"json,omitempty"`
	Trim bool `json:"trim,omitempty"`
}

type screenResult struct {
	Lines []string `json:"lines"`
}

type sendParams struct {
	Input  string `json:"input"`
	Escape bool   `json:"escape,omitempty"`
}

type waitParams struct {
	Text    string `json:"text"`
	Timeout string `json:"timeout"`
	Regex   bool   `json:"regex,omitempty"`
	Not     bool   `json:"not,omitempty"`
}

type resizeParams struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type statusResult struct {
	Running bool `json:"running"`
	Cols    int  `json:"cols"`
	Rows    int  `json:"rows"`
}

func socketPath(name string) string {
	return filepath.Join("/tmp", fmt.Sprintf("nxtest-%s.sock", name))
}

// ipcCall connects to the daemon, sends a request, and returns the response.
func ipcCall(name string, req *ipcRequest) (*ipcResponse, error) {
	conn, err := net.Dial("unix", socketPath(name))
	if err != nil {
		return nil, fmt.Errorf("connect to daemon %q: %w (is it running?)", name, err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	// Signal that we're done writing so the daemon can detect EOF
	// after reading the single request line.
	if uc, ok := conn.(*net.UnixConn); ok {
		uc.CloseWrite()
	}

	var resp ipcResponse
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &resp, nil
}
