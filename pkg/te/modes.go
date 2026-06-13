package te

// Terminal modes are stored in Screen.Mode keyed by an int. ANSI modes (set
// via CSI Pn h) use their raw number. DEC private modes (set via CSI ? Pn h)
// share the same map, so to avoid colliding with ANSI numbers they are encoded
// as (number << privateModeShift); use PrivateMode to build the key rather
// than open-coding the shift.
const privateModeShift = 5

// PrivateMode returns the Screen.Mode key for DEC private mode n (the mode
// number used in CSI ? n h / l). Callers that probe Screen.Mode for a private
// mode should index it with PrivateMode(n) rather than hand-rolling the
// encoding.
func PrivateMode(n int) int { return n << privateModeShift }

const (
	// ModeLNM is a terminal mode constant.
	ModeLNM = 20
	// ModeIRM is a terminal mode constant.
	ModeIRM = 4

	// ModeDECTCEM is a terminal mode constant.
	ModeDECTCEM = 25 << 5
	// ModeDECSCNM is a terminal mode constant.
	ModeDECSCNM = 5 << 5
	// ModeDECOM is a terminal mode constant.
	ModeDECOM = 6 << 5
	// ModeDECAWM is a terminal mode constant.
	ModeDECAWM = 7 << 5
	// ModeDECCOLM is a terminal mode constant.
	ModeDECCOLM = 3 << 5
	// ModeDECLRMM is a terminal mode constant.
	ModeDECLRMM = 69 << 5
	// ModeDECCKM is a terminal mode constant.
	ModeDECCKM = 1 << 5
	// ModeDECSCLM is a terminal mode constant.
	ModeDECSCLM = 4 << 5
	// ModeDECARM is a terminal mode constant.
	ModeDECARM = 8 << 5
	// ModeDECPFF is a terminal mode constant.
	ModeDECPFF = 18 << 5
	// ModeDECPEX is a terminal mode constant.
	ModeDECPEX = 19 << 5
	// ModeDECNRCM is a terminal mode constant.
	ModeDECNRCM = 42 << 5
	// ModeDECNKM is a terminal mode constant.
	ModeDECNKM = 66 << 5
	// ModeDECBKM is a terminal mode constant.
	ModeDECBKM = 67 << 5
	// ModeDECKBUM is a terminal mode constant.
	ModeDECKBUM = 68 << 5
	// ModeDECNCSM is a terminal mode constant.
	ModeDECNCSM = 95 << 5
	// ModeDECOSCNM is a terminal mode constant.
	ModeDECOSCNM = 106 << 5
	// ModeDECRLM is a terminal mode constant.
	ModeDECRLM = 34 << 5
	// ModeDECHCCM is a terminal mode constant.
	ModeDECHCCM = 60 << 5
	// ModeDECAAM is a terminal mode constant.
	ModeDECAAM = 100 << 5
	// ModeDECCANSM is a terminal mode constant.
	ModeDECCANSM = 101 << 5
	// ModeDECNULM is a terminal mode constant.
	ModeDECNULM = 102 << 5
	// ModeDECHDPXM is a terminal mode constant.
	ModeDECHDPXM = 103 << 5
	// ModeDECESKM is a terminal mode constant.
	ModeDECESKM = 104 << 5
	// ModeReverseWrapInline is a terminal mode constant.
	ModeReverseWrapInline = 45 << 5
	// ModeReverseWrapExtend is a terminal mode constant.
	ModeReverseWrapExtend = 1045 << 5
	// ModeDECSaveCursor is a terminal mode constant.
	ModeDECSaveCursor = 1048 << 5
	// ModeAltBuf is a terminal mode constant.
	ModeAltBuf = 47 << 5
	// ModeAltBufOpt is a terminal mode constant.
	ModeAltBufOpt = 1047 << 5
	// ModeAltBufCursor is a terminal mode constant.
	ModeAltBufCursor = 1049 << 5
	// ModeAllow80To132 is a terminal mode constant.
	ModeAllow80To132 = 40 << 5
	// ModeMoreFix is a terminal mode constant.
	ModeMoreFix = 41 << 5

	// Mouse tracking and bracketed-paste private modes. Consumers (e.g. the
	// TUI, which decides how to forward mouse and paste input) previously
	// recreated these numbers locally.
	ModeMouseX10         = 9 << 5    // X10 compatibility mouse reporting
	ModeMouseNormal      = 1000 << 5 // normal mouse tracking (press/release)
	ModeMouseButtonEvent = 1002 << 5 // button-event tracking (drag)
	ModeMouseAnyEvent    = 1003 << 5 // any-event tracking (all motion)
	ModeMouseSGR         = 1006 << 5 // SGR extended mouse coordinate encoding
	ModeFocusEvent       = 1004 << 5 // focus in/out reporting
	ModeBracketedPaste   = 2004 << 5 // bracketed paste
)
