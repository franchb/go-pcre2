package pcre2

import (
	"errors"
	"fmt"
)

// Errors returned when a match exceeds one of its limits. See MatchLimit,
// DepthLimit and HeapLimit.
var (
	ErrMatchLimit = errors.New("pcre2: match limit exceeded")
	ErrDepthLimit = errors.New("pcre2: depth limit exceeded")
	ErrHeapLimit  = errors.New("pcre2: heap limit exceeded")
	ErrNoMemory   = errors.New("pcre2: out of memory")
)

// CompileError reports a pattern that PCRE2 rejected.
type CompileError struct {
	Pattern string
	Offset  int // byte offset in Pattern where PCRE2 stopped
	Code    int // PCRE2 error code
	Message string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("pcre2: compile %q at offset %d: %s", e.Pattern, e.Offset, e.Message)
}

// MatchError reports a PCRE2 match failure other than "no match" and the
// limit errors above, for example an invalid start offset.
type MatchError struct {
	Code    int
	Message string
}

func (e *MatchError) Error() string {
	return fmt.Sprintf("pcre2: match error %d: %s", e.Code, e.Message)
}

// TrapError reports that the WebAssembly module trapped, for example on an
// out-of-bounds access inside its linear memory. The instance that trapped
// is discarded; the Regexp stays usable.
type TrapError struct {
	Value any
}

func (e *TrapError) Error() string {
	return fmt.Sprintf("pcre2: wasm trap: %v", e.Value)
}
