//go:build js
// +build js

package tea

func (p *Program) initInput() error { return nil }

const suspendSupported = false

func suspendProcess() {}
