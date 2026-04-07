package transport

// execAddr is a synthetic net.Addr returned by execConn.LocalAddr /
// RemoteAddr. It carries a human-readable label only — there is no
// underlying network endpoint. Used for diagnostics in logs and the
// ssh:// transport's PTY-spawned ssh subprocess.
type execAddr string

func (a execAddr) Network() string { return "exec" }
func (a execAddr) String() string  { return string(a) }
