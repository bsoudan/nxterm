//go:build js
// +build js

package tea

func (p *Program) listenForResize(done chan struct{}) {
	close(done)
}
