package config

import (
	"strings"
	"sync"
)

var (
	traceFlags map[string]bool
	traceMu    sync.RWMutex
)

// SetTraceFlags parses and stores trace flags. Each input string may
// contain multiple flags separated by commas and/or spaces. Can be
// called multiple times — flags accumulate.
func SetTraceFlags(flags ...string) {
	traceMu.Lock()
	defer traceMu.Unlock()
	if traceFlags == nil {
		traceFlags = make(map[string]bool)
	}
	for _, f := range flags {
		for _, part := range strings.FieldsFunc(f, func(r rune) bool {
			return r == ',' || r == ' '
		}) {
			if s := strings.TrimSpace(part); s != "" {
				traceFlags[s] = true
			}
		}
	}
}

// TraceEnabled reports whether the named trace flag is active.
func TraceEnabled(name string) bool {
	traceMu.RLock()
	defer traceMu.RUnlock()
	return traceFlags[name]
}
