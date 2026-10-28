package pcre2

// Compile options, from pcre2.h.
const (
	optCaseless        = 0x00000008
	optDotAll          = 0x00000020
	optExtended        = 0x00000080
	optMultiline       = 0x00000400
	optUCP             = 0x00020000
	optUTF             = 0x00080000
	optMatchInvalidUTF = 0x04000000
)

type config struct {
	flags      uint32
	matchLimit uint32 // 0 means the PCRE2 default
	depthLimit uint32
	heapLimit  uint32 // KiB
}

func defaultConfig() config {
	// Go strings are UTF-8 by convention but may hold any bytes.
	// PCRE2_MATCH_INVALID_UTF lets invalid sequences in the subject
	// fail to match instead of failing the call.
	return config{flags: optUTF | optMatchInvalidUTF}
}

// An Option changes how a pattern is compiled or matched.
type Option func(*config)

// Caseless matches letters regardless of case, like (?i).
func Caseless() Option { return func(c *config) { c.flags |= optCaseless } }

// DotAll makes . match newlines too, like (?s).
func DotAll() Option { return func(c *config) { c.flags |= optDotAll } }

// Extended ignores unescaped whitespace and # comments in the pattern, like (?x).
func Extended() Option { return func(c *config) { c.flags |= optExtended } }

// Multiline makes ^ and $ match at line boundaries, like (?m).
func Multiline() Option { return func(c *config) { c.flags |= optMultiline } }

// UCP makes \d, \s, \w and POSIX classes use Unicode properties instead of
// ASCII. Without it they match ASCII only, as in Go's regexp.
func UCP() Option { return func(c *config) { c.flags |= optUCP } }

// Bytes treats the pattern and subjects as bytes instead of UTF-8, so a
// pattern such as \xff matches the single byte 0xff.
func Bytes() Option {
	return func(c *config) { c.flags &^= optUTF | optMatchInvalidUTF | optUCP }
}

// MatchLimit caps how many times pcre2_match goes round its main matching
// loop during one call, which bounds backtracking. Exceeding it returns
// ErrMatchLimit. PCRE2's default is 10,000,000.
func MatchLimit(n uint32) Option { return func(c *config) { c.matchLimit = n } }

// DepthLimit caps the depth of nested backtracking in one match, and so
// indirectly its memory. Exceeding it returns ErrDepthLimit.
func DepthLimit(n uint32) Option { return func(c *config) { c.depthLimit = n } }

// HeapLimit caps the heap memory, in KiB, that one match may use to hold
// backtracking information. Exceeding it returns ErrHeapLimit.
func HeapLimit(kib uint32) Option { return func(c *config) { c.heapLimit = kib } }
