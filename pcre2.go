// Package pcre2 provides PCRE2 regular expressions for Go without cgo.
//
// PCRE2 is compiled to WebAssembly and translated to Go by wasm2go, so the
// library builds with CGO_ENABLED=0 and cross-compiles like any Go package.
// The module has no imports: matching cannot reach files, the network, the
// clock or the process.
//
// Patterns use PCRE2 syntax, including features Go's regexp and
// dlclark/regexp2 lack: \K, recursion (?R), subroutine calls (?1) and
// (?&name), and branch reset (?|...). PCRE2 is a backtracking engine: set
// MatchLimit on patterns that see untrusted input, and handle the errors
// the match methods return.
//
// Building with -tags pcre2_cgo replaces the wasm2go backend with cgo
// bindings to a native PCRE2 with the JIT, behind the same API.
//
// Offsets are byte offsets into the subject, as in Go's regexp. Subjects
// must be valid UTF-8 unless the pattern was compiled with Bytes or
// InvalidUTF; otherwise matching returns a *MatchError.
package pcre2

import (
	"errors"
	"sync"
)

const (
	errNoMatch    = -1
	errMatchLimit = -47
	errNoMemory   = -48
	errDepthLimit = -53
	errHeapLimit  = -63

	infoCaptureCount  = 4
	infoNameCount     = 17
	infoNameEntrySize = 18
	infoNameTable     = 19

	matchNotEmptyAtStart = 0x00000008
	matchAnchored        = 0x80000000
	matchNoUTFCheck      = 0x40000000
)

// Regexp is a compiled PCRE2 pattern. It is safe for concurrent use.
//
// Each goroutine that matches at the same time gets its own instance with
// its own copy of the compiled pattern. Instances are pooled and released
// to the garbage collector with the pool, so there is no Close.
type Regexp struct {
	expr  string
	cfg   config
	names []string // names[i] is the name of group i, "" if unnamed
	pool  sync.Pool
}

// Compile parses a PCRE2 pattern. By default the pattern and subjects are
// UTF-8; see Bytes.
func Compile(expr string, opts ...Option) (*Regexp, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	re := &Regexp{expr: expr, cfg: cfg}
	in, err := re.newInstance()
	if err != nil {
		return nil, err
	}
	re.names = in.groupNames()
	re.pool.Put(in)
	return re, nil
}

// MustCompile is like Compile but panics if the pattern does not compile.
func MustCompile(expr string, opts ...Option) *Regexp {
	re, err := Compile(expr, opts...)
	if err != nil {
		panic(err)
	}
	return re
}

// String returns the source pattern.
func (re *Regexp) String() string { return re.expr }

// NumSubexp returns the number of capturing groups.
func (re *Regexp) NumSubexp() int { return len(re.names) - 1 }

// SubexpNames returns the names of the capturing groups. Element 0 is the
// whole match and is always "". Unnamed groups are "".
func (re *Regexp) SubexpNames() []string { return re.names }

// SubexpIndex returns the number of the group with the given name, or -1.
// With duplicate names (allowed with (?J) or (?|...)) it returns the first.
func (re *Regexp) SubexpIndex(name string) int {
	if name == "" {
		return -1
	}
	for i, n := range re.names {
		if n == name {
			return i
		}
	}
	return -1
}

// MatchString reports whether s contains a match.
func (re *Regexp) MatchString(s string) (bool, error) {
	loc, err := find(re, s, 0, 0)
	return loc != nil, err
}

// Match reports whether b contains a match.
func (re *Regexp) Match(b []byte) (bool, error) {
	loc, err := find(re, b, 0, 0)
	return loc != nil, err
}

// FindStringIndex returns the start and end of the leftmost match in s, or
// nil if there is none.
func (re *Regexp) FindStringIndex(s string) ([]int, error) {
	loc, err := find(re, s, 0, 0)
	if loc == nil {
		return nil, err
	}
	return loc[:2], nil
}

// FindIndex is FindStringIndex for a byte slice.
func (re *Regexp) FindIndex(b []byte) ([]int, error) {
	loc, err := find(re, b, 0, 0)
	if loc == nil {
		return nil, err
	}
	return loc[:2], nil
}

// FindString returns the text of the leftmost match in s, or "" if there is
// none. Use FindStringIndex to tell an empty match from no match.
func (re *Regexp) FindString(s string) (string, error) {
	loc, err := find(re, s, 0, 0)
	if loc == nil {
		return "", err
	}
	return s[loc[0]:loc[1]], nil
}

// FindStringSubmatchIndex returns index pairs for the leftmost match and
// its groups: loc[2*i:2*i+2] is group i. Groups that did not take part are
// -1, -1. It returns nil if there is no match.
func (re *Regexp) FindStringSubmatchIndex(s string) ([]int, error) {
	return find(re, s, 0, 0)
}

// FindSubmatchIndex is FindStringSubmatchIndex for a byte slice.
func (re *Regexp) FindSubmatchIndex(b []byte) ([]int, error) {
	return find(re, b, 0, 0)
}

// FindStringSubmatch returns the text of the leftmost match and its groups.
// Groups that did not take part are "". It returns nil if there is no match.
func (re *Regexp) FindStringSubmatch(s string) ([]string, error) {
	loc, err := find(re, s, 0, 0)
	if loc == nil {
		return nil, err
	}
	out := make([]string, len(loc)/2)
	for i := range out {
		if loc[2*i] >= 0 {
			out[i] = s[loc[2*i]:loc[2*i+1]]
		}
	}
	return out, nil
}

// FindAllStringSubmatchIndex returns up to n successive matches (all of them
// if n < 0), each in the form FindStringSubmatchIndex returns.
//
// After an empty match, the next attempt starts at the same position but
// must not be empty; if that fails, it moves on one character. This is
// Perl's behaviour, and differs from Go's regexp, which never reports an
// empty match right after a previous match.
func (re *Regexp) FindAllStringSubmatchIndex(s string, n int) ([][]int, error) {
	return findAll(re, s, n)
}

// FindAllSubmatchIndex is FindAllStringSubmatchIndex for a byte slice.
func (re *Regexp) FindAllSubmatchIndex(b []byte, n int) ([][]int, error) {
	return findAll(re, b, n)
}

// FindAllStringIndex returns the start and end of up to n successive
// matches (all of them if n < 0). See FindAllStringSubmatchIndex.
func (re *Regexp) FindAllStringIndex(s string, n int) ([][]int, error) {
	all, err := findAll(re, s, n)
	for i, loc := range all {
		all[i] = loc[:2]
	}
	return all, err
}

// FindAllString returns the text of up to n successive matches (all of them
// if n < 0). See FindAllStringSubmatchIndex.
func (re *Regexp) FindAllString(s string, n int) ([]string, error) {
	all, err := findAll(re, s, n)
	if all == nil {
		return nil, err
	}
	out := make([]string, len(all))
	for i, loc := range all {
		out[i] = s[loc[0]:loc[1]]
	}
	return out, err
}

type subject interface{ ~string | ~[]byte }

func find[T subject](re *Regexp, s T, start int, opts uint32) (loc []int, err error) {
	in, err := re.get()
	if err != nil {
		return nil, err
	}
	defer re.put(in, &err)
	load(in, s)
	return in.match(len(s), start, opts)
}

func findAll[T subject](re *Regexp, s T, n int) (all [][]int, err error) {
	if n == 0 {
		return nil, nil
	}
	in, err := re.get()
	if err != nil {
		return nil, err
	}
	defer re.put(in, &err)
	load(in, s)

	// The first call validates the whole subject as UTF-8; later calls
	// start on character boundaries, so they can skip the check. PCRE2
	// ignores the flag under PCRE2_MATCH_INVALID_UTF.
	var noCheck uint32
	start, opts := 0, uint32(0)
	for start <= len(s) && (n < 0 || len(all) < n) {
		loc, err := in.match(len(s), start, opts|noCheck)
		if err != nil {
			return all, err
		}
		noCheck = matchNoUTFCheck
		if loc == nil {
			if opts == 0 {
				break
			}
			// The non-empty retry at an empty match failed: move on one
			// character and try again normally.
			start = nextChar(s, start, re.cfg.flags&optUTF != 0)
			opts = 0
			continue
		}
		all = append(all, loc)
		start, opts = loc[1], 0
		if loc[0] == loc[1] {
			opts = matchNotEmptyAtStart | matchAnchored
		}
	}
	return all, nil
}

// nextChar returns the offset of the character after s[i:].
func nextChar[T subject](s T, i int, utf bool) int {
	i++
	for utf && i < len(s) && s[i]&0xc0 == 0x80 {
		i++
	}
	return i
}

func (re *Regexp) get() (*instance, error) {
	if in, ok := re.pool.Get().(*instance); ok {
		return in, nil
	}
	return re.newInstance()
}

// put returns in to the pool, unless the call trapped. A trap is turned
// into a *TrapError and the instance, whose memory may be inconsistent, is
// dropped.
func (re *Regexp) put(in *instance, err *error) {
	if v := recover(); v != nil {
		*err = trapError(v)
		return
	}
	in.unload()
	re.pool.Put(in)
}

// matchErr maps a negative pcre2_match result to an error. No match is
// not an error.
func matchErr(in *instance, rc int32) error {
	switch {
	case rc >= 0 || rc == errNoMatch:
		return nil
	case rc == errMatchLimit:
		return ErrMatchLimit
	case rc == errDepthLimit:
		return ErrDepthLimit
	case rc == errHeapLimit:
		return ErrHeapLimit
	case rc == errNoMemory:
		return ErrNoMemory
	default:
		return &MatchError{Code: int(rc), Message: in.errorMessage(rc)}
	}
}

func trapError(v any) error {
	if e, ok := v.(error); ok && errors.Is(e, ErrNoMemory) {
		return ErrNoMemory
	}
	return &TrapError{Value: v}
}
