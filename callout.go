package pcre2

import (
	"errors"
	"unsafe"
)

// A Callout describes the state of a match at a callout point: (?C), (?Cn)
// or (?C"text") in the pattern, or every item with AutoCallout. See
// pcre2callout(3) for the full rules, including the optimizations
// that can skip callouts.
//
// The Callout is reused between calls: a CalloutFunc must not keep it, or
// call its methods, after it returns. Its strings may be kept.
type Callout struct {
	// Number is n in (?Cn), 0 for (?C"text") and 255 for AutoCallout.
	Number int

	// String is the text of a (?C"text") callout, without its delimiters,
	// and "" for numbered callouts.
	String string

	// Subject is the subject being matched. For a []byte subject it shares
	// memory with the caller's slice.
	Subject string

	StartMatch      int // where this match attempt started, after any \K
	Position        int // the current position in the subject
	PatternPosition int // offset in the pattern of the next item
	NextItemLength  int // length of the next item in the pattern

	// CaptureTop is one more than the highest group captured so far, and
	// CaptureLast the most recently captured group, or 0 if none.
	CaptureTop  int
	CaptureLast int

	// Mark is the name of the most recent (*MARK), (*PRUNE) or (*THEN)
	// with a name, or "" if there is none.
	Mark string

	// NewStart is set for the first callout at each new start position,
	// and Backtracked if the match has backtracked since the last callout.
	// PCRE2's JIT does not report them, so they are always false in the
	// cgo backend unless the pattern was compiled with NoJIT.
	NewStart    bool
	Backtracked bool

	ovec []int // offset pairs, as read from the callout block
}

// Group returns the offsets of group i as captured so far, or -1, -1 if it
// has not been captured. Group 0, the whole match, is never complete at a
// callout, so it is always -1, -1.
func (c *Callout) Group(i int) (start, end int) {
	if i <= 0 || i >= c.CaptureTop || 2*i+1 >= len(c.ovec) {
		return -1, -1
	}
	return c.ovec[2*i], c.ovec[2*i+1]
}

// A CalloutFunc is called at each callout point. It returns nil to go on
// matching, Fail to fail at this point and backtrack, as if a lookahead
// had failed, or another error to abandon the match. The match method then
// returns that error unchanged.
//
// A CalloutFunc is called from every goroutine that matches the pattern,
// so it may run concurrently with itself. If it panics, the panic reaches
// the caller of the match method.
type CalloutFunc func(c *Callout) error

// Fail is returned by a CalloutFunc to make the match fail at the current
// point and backtrack. It is never returned by a match method.
var Fail = errors.New("pcre2: callout failed this match path")

const (
	calloutContinue = 0
	calloutFail     = 1
	errCallout      = -37 // PCRE2_ERROR_CALLOUT, reserved for callouts

	calloutStartMatch = 0x1 // PCRE2_CALLOUT_STARTMATCH
	calloutBacktrack  = 0x2 // PCRE2_CALLOUT_BACKTRACK
)

// callouts is the per-instance state behind a CalloutFunc. It does not
// point back at the instance, so the cgo backend can hold it in a
// cgo.Handle without keeping the instance alive.
type callouts struct {
	fn      CalloutFunc
	subject string // the subject of the current call
	c       Callout

	// The outcome of the last callout that stopped the match.
	err      error
	panicked bool
	panicVal any

	strs map[int]string // callout strings, by offset in the pattern
}

// call runs fn on c, which the backend has filled in, and returns the
// value for PCRE2. A panic is recovered here, so that it does not unwind
// through PCRE2, and raised again once pcre2_match has returned.
func (co *callouts) call() (rc int32) {
	defer func() {
		if v := recover(); v != nil {
			co.panicked, co.panicVal = true, v
			rc = errCallout
		}
	}()
	switch err := co.fn(&co.c); {
	case err == nil:
		return calloutContinue
	case errors.Is(err, Fail):
		return calloutFail
	default:
		co.err = err
		return errCallout
	}
}

// str returns the callout string at offset off in the pattern, reading it
// with read the first time.
func (co *callouts) str(off int, read func() string) string {
	s, ok := co.strs[off]
	if !ok {
		if co.strs == nil {
			co.strs = map[int]string{}
		}
		s = read()
		co.strs[off] = s
	}
	return s
}

// finish is called after each pcre2_match. It returns the error that
// stopped the match, if a callout did, and raises a panic from the
// CalloutFunc as a calloutPanic.
func (co *callouts) finish(rc int32) error {
	err, panicked, v := co.err, co.panicked, co.panicVal
	co.err, co.panicked, co.panicVal = nil, false, nil
	if rc != errCallout {
		return nil
	}
	if panicked {
		panic(calloutPanic{v})
	}
	return err
}

// calloutPanic carries a panic from a CalloutFunc past the recover in put,
// which would otherwise turn it into a *TrapError.
type calloutPanic struct{ v any }

// subjectString returns s as a string without copying. For a []byte it
// shares the slice's memory, which Callout.Subject documents.
func subjectString[T subject](s T) string {
	if b, ok := any(s).([]byte); ok {
		return unsafe.String(unsafe.SliceData(b), len(b))
	}
	return string(s)
}
