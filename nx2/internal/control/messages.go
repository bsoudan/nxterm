// Package control defines nx2 control-plane messages (the JSON carried in
// wire.Control frames): surface lifecycle and content-addressed app selection.
// The data plane is opaque and never touches this package.
package control

import "encoding/json"

// Type tags a control message.
type Type string

const (
	// TypeAnnounce (broker->host): which app should drive a surface.
	TypeAnnounce Type = "announce"
	// TypeFetch (host->broker): request an app's WASM module by hash.
	TypeFetch Type = "fetch"
	// TypeChunk (broker->host): one chunk of a fetched module.
	TypeChunk Type = "chunk"
	// TypeSelectApp (host->broker): bind an app to a surface; broker starts the
	// app's server-side companion.
	TypeSelectApp Type = "select_app"
	// TypeSelected (broker->host): result of a select_app.
	TypeSelected Type = "selected"
)

// AppID names an app by content hash (of its WASM module) plus an optional label.
type AppID struct {
	Hash string `json:"hash"`
	Name string `json:"name,omitempty"`
}

// Announce tells the host which app drives a surface.
type Announce struct {
	Surface uint32 `json:"surface"`
	App     AppID  `json:"app"`
}

// Fetch requests the WASM module identified by Hash.
type Fetch struct {
	Hash string `json:"hash"`
}

// Chunk carries part of a fetched module. Data is base64-encoded by JSON. Done
// marks the final chunk; Error/Message report a failed fetch.
type Chunk struct {
	Hash    string `json:"hash"`
	Data    []byte `json:"data,omitempty"`
	Done    bool   `json:"done,omitempty"`
	Error   bool   `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// Envelope is the on-wire control message: a type tag plus a typed payload.
type Envelope struct {
	Type    Type            `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// SelectApp asks the broker to run app App for the given surface. For the spike
// App is a registry name; it will become a content hash (see control.wit).
// Session names a shared instance: hosts that select the same (App, Session)
// attach to one companion (multi-client / reconnect).
type SelectApp struct {
	Surface uint32 `json:"surface"`
	App     string `json:"app"`
	Session string `json:"session,omitempty"`
}

// Selected reports the outcome of a SelectApp.
type Selected struct {
	Surface uint32 `json:"surface"`
	Error   bool   `json:"error"`
	Message string `json:"message,omitempty"`
}

// Marshal encodes v as an Envelope of the given type.
func Marshal(t Type, v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{Type: t, Payload: raw})
}

// Parse splits a control frame into its type and raw payload.
func Parse(b []byte) (Type, json.RawMessage, error) {
	var e Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return "", nil, err
	}
	return e.Type, e.Payload, nil
}
