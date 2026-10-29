package pcre2

import (
	"bufio"
	"errors"
	"flag"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// Fuzz targets; see docs/FUZZING.md. Every pattern is compiled with low
// limits, so that each input finishes quickly: Go's fuzzer has no
// per-input timeout.

const (
	fuzzMatchLimit = 100_000
	fuzzDepthLimit = 1_000
	fuzzHeapLimit  = 4 << 10 // KiB
)

// Bits of the flags argument of the fuzz targets.
const (
	fuzzCaseless = 1 << iota
	fuzzDotAll
	fuzzExtended
	fuzzMultiline
	fuzzUCP
	fuzzBytes
	fuzzInvalidUTF
	fuzzAutoCallout
)

func fuzzOptions(flags uint8) []Option {
	limits := []Option{MatchLimit(fuzzMatchLimit), DepthLimit(fuzzDepthLimit), HeapLimit(fuzzHeapLimit)}
	return append(limits, fuzzFlags(flags)...)
}

// fuzzFlags maps the flags argument to compile options.
func fuzzFlags(flags uint8) []Option {
	var opts []Option
	for _, f := range []struct {
		bit uint8
		opt Option
	}{
		{fuzzCaseless, Caseless()},
		{fuzzDotAll, DotAll()},
		{fuzzExtended, Extended()},
		{fuzzMultiline, Multiline()},
		{fuzzUCP, UCP()},
		{fuzzInvalidUTF, InvalidUTF()},
	} {
		if flags&f.bit != 0 {
			opts = append(opts, f.opt)
		}
	}
	// Bytes clears what UCP and InvalidUTF set, so it goes last.
	if flags&fuzzBytes != 0 {
		opts = append(opts, Bytes())
	}
	return opts
}

// fuzzSeed is one pattern with a subject, from the tests or from PCRE2's
// own test data.
type fuzzSeed struct {
	pattern, subject string
	flags            uint8
}

var testSeeds = []fuzzSeed{
	{`foo\Kbar`, "xfoobar", 0},
	{`\((?:[^()]++|(?R))*\)`, "x(a(b)c)y", 0},
	{`(?<pair>\d\d)-(?&pair)`, "ab 12-34", 0},
	{`(?|(a)|(b))\1`, "xbb", 0},
	{`a++b`, "aaab", 0},
	{`(?<=\$)\d+`, "cost $42", 0},
	{`\p{Greek}+`, "abc αβγ", fuzzUCP},
	{`a*`, "baaab", 0},
	{`(?<year>\d{4})-(?<month>\d\d)`, "on 2026-10", fuzzCaseless},
	{`(a+)+$`, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaab", 0},
	{`\xff`, "\xff", fuzzBytes},
	{`.`, "a\xffb", fuzzInvalidUTF},
	{`(*MARK:m)a(?C1)b(?C"two")`, "ab", 0},
	{`ab`, "ab", fuzzAutoCallout},
	{`^\d{4}-\d\d-\d\d$`, "2026-10-29", 0},
	{`[\w.]+@[\w.]+\.com`, "user1@example.com paid", 0},
	{`\b(\w+) \1\b`, "the the order", 0},
	{`(?m)^$`, "\n\n", fuzzMultiline},
}

// pcre2Seeds reads testdata/pcre2-seeds.txt, which buildtools/fuzzseeds
// extracts from PCRE2's testdata/testinput* files. A pattern line is "/",
// the flags in decimal, a space and the quoted pattern; each of its
// subject lines is a tab and the quoted subject. Other lines are comments.
func pcre2Seeds(tb testing.TB) []fuzzSeed {
	f, err := os.Open("testdata/pcre2-seeds.txt")
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	var seeds []fuzzSeed
	var pattern string
	var flags uint8
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "/"):
			num, quoted, _ := strings.Cut(line[1:], " ")
			n, err := strconv.ParseUint(num, 10, 8)
			if err != nil {
				tb.Fatalf("pcre2-seeds.txt: %q: %v", line, err)
			}
			pattern, flags = unquoteSeed(tb, quoted), uint8(n)
			seeds = append(seeds, fuzzSeed{pattern, "", flags})
		case strings.HasPrefix(line, "\t"):
			seeds = append(seeds, fuzzSeed{pattern, unquoteSeed(tb, line[1:]), flags})
		}
	}
	if err := sc.Err(); err != nil {
		tb.Fatal(err)
	}
	return seeds
}

func unquoteSeed(tb testing.TB, q string) string {
	s, err := strconv.Unquote(q)
	if err != nil {
		tb.Fatalf("pcre2-seeds.txt: %s: %v", q, err)
	}
	return s
}

// addSeeds adds the test seeds and, except under the race detector when
// not fuzzing, PCRE2's: they take about 5 s per target as plain tests,
// several times that with -race.
func addSeeds(f *testing.F, add func(fuzzSeed)) {
	for _, s := range testSeeds {
		add(s)
	}
	if raceEnabled && flag.Lookup("test.fuzz").Value.String() == "" {
		return
	}
	for _, s := range pcre2Seeds(f) {
		add(s)
	}
}

func FuzzCompile(f *testing.F) {
	addSeeds(f, func(s fuzzSeed) { f.Add(s.pattern, s.flags) })
	f.Fuzz(func(t *testing.T, pattern string, flags uint8) {
		t.Logf("pattern %q, flags %#x", pattern, flags)
		re, err := Compile(pattern, fuzzOptions(flags)...)
		if err != nil {
			checkCompileError(t, pattern, err)
			return
		}
		names := re.SubexpNames()
		if len(names) != re.NumSubexp()+1 || names[0] != "" {
			t.Fatalf("SubexpNames() = %q with NumSubexp() = %d", names, re.NumSubexp())
		}
		for i, name := range names {
			if name == "" {
				continue
			}
			// SubexpIndex returns the first group with the name.
			if j := re.SubexpIndex(name); j < 0 || j > i || names[j] != name {
				t.Fatalf("SubexpIndex(%q) = %d, but group %d has that name", name, j, i)
			}
		}
	})
}

func FuzzMatch(f *testing.F) {
	addSeeds(f, func(s fuzzSeed) { f.Add(s.pattern, s.subject, s.flags) })
	f.Fuzz(func(t *testing.T, pattern, subject string, flags uint8) {
		t.Logf("pattern %q, subject %q, flags %#x", pattern, subject, flags)
		re, err := Compile(pattern, fuzzOptions(flags)...)
		if err != nil {
			checkCompileError(t, pattern, err)
			return
		}
		got := checkMatch(t, re, subject)

		// The same results from a fresh Regexp: nothing leaks between
		// calls through the pooled instance, after errors included.
		fresh := MustCompile(pattern, fuzzOptions(flags)...)
		loc, err := fresh.FindStringSubmatchIndex(subject)
		checkErr(t, err)
		if !sameResult(got.loc, got.err, loc, err) {
			t.Fatalf("pooled Regexp gave %v, %v; a fresh one %v, %v", got.loc, got.err, loc, err)
		}
	})
}

func FuzzCallout(f *testing.F) {
	addSeeds(f, func(s fuzzSeed) { f.Add(s.pattern, s.subject, s.flags, []byte{0}) })
	f.Add(`(\d)(?C1)`, "1234", uint8(0), []byte{0, 1, 0, 1})
	f.Add(`a(?C1)`, "aaa", uint8(0), []byte{0, 2})
	f.Add(`a(?C1)`, "aaa", uint8(0), []byte{3})
	f.Fuzz(func(t *testing.T, pattern, subject string, flags uint8, script []byte) {
		t.Logf("pattern %q, subject %q, flags %#x, script %v", pattern, subject, flags, script)
		if len(script) == 0 {
			script = []byte{0}
		}
		var calls int
		fn := func(c *Callout) error {
			checkCallout(t, c, subject)
			act := script[calls%len(script)]
			calls++
			if calls > 10_000 {
				return errCalloutBudget
			}
			switch act % 8 {
			case 5:
				return Fail
			case 6:
				return errCalloutAbort
			case 7:
				panic(calloutPanicValue)
			}
			return nil
		}
		opts := fuzzOptions(flags)
		if flags&fuzzAutoCallout != 0 {
			opts = append(opts, AutoCallout())
		}
		re, err := Compile(pattern, append(opts, OnCallout(fn))...)
		if err != nil {
			checkCompileError(t, pattern, err)
			return
		}

		loc, err, panicked := findCatching(re, subject)
		if panicked != nil {
			if panicked != calloutPanicValue {
				t.Fatalf("recovered %v, want the CalloutFunc's panic value", panicked)
			}
		} else {
			checkErr(t, err)
			checkLoc(t, re, subject, loc)
		}

		// After any outcome, the Regexp matches like one whose callouts
		// all continue, which matches like one with no callouts.
		calls, script = 0, []byte{0}
		after, errAfter, p := findCatching(re, subject)
		if p != nil {
			t.Fatalf("panic %v from a CalloutFunc that does not panic", p)
		}
		plain := MustCompile(pattern, opts...)
		want, errWant := plain.FindStringSubmatchIndex(subject)
		if errors.Is(errAfter, errCalloutBudget) {
			return // too many callouts to finish; nothing to compare
		}
		if !sameResult(after, errAfter, want, errWant) {
			t.Fatalf("with continuing callouts %v, %v; without callouts %v, %v", after, errAfter, want, errWant)
		}
	})
}

var (
	errCalloutAbort   = errors.New("fuzz: callout abort")
	errCalloutBudget  = errors.New("fuzz: callout budget spent")
	calloutPanicValue = &struct{ name string }{"fuzz: callout panic"}
)

func findCatching(re *Regexp, s string) (loc []int, err error, panicked any) {
	defer func() { panicked = recover() }()
	loc, err = re.FindStringSubmatchIndex(s)
	return loc, err, nil
}

func checkCallout(t *testing.T, c *Callout, subject string) {
	t.Helper()
	n := len(subject)
	if c.Subject != subject {
		t.Fatalf("Callout.Subject = %q, want %q", c.Subject, subject)
	}
	if c.Position < 0 || c.Position > n || c.StartMatch < 0 || c.StartMatch > n {
		t.Fatalf("Callout positions out of range: Position %d, StartMatch %d, subject length %d", c.Position, c.StartMatch, n)
	}
	if c.CaptureTop < 1 || c.CaptureLast < 0 {
		t.Fatalf("CaptureTop %d, CaptureLast %d", c.CaptureTop, c.CaptureLast)
	}
	for i := range c.CaptureTop {
		s, e := c.Group(i)
		if (s == -1) != (e == -1) || s < -1 || s > e || e > n {
			t.Fatalf("Callout.Group(%d) = %d, %d, subject length %d", i, s, e, n)
		}
	}
}

func checkCompileError(t *testing.T, pattern string, err error) {
	t.Helper()
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("Compile(%q) returned %T %v, want a *CompileError", pattern, err, err)
	}
	if ce.Offset < 0 || ce.Offset > len(pattern) {
		t.Fatalf("CompileError.Offset = %d, pattern length %d", ce.Offset, len(pattern))
	}
}

// checkErr fails on the errors a match must never return.
func checkErr(t *testing.T, err error) {
	t.Helper()
	var te *TrapError
	if errors.As(err, &te) {
		t.Fatalf("match trapped: %v", err)
	}
	var me *MatchError
	switch {
	case err == nil, errors.As(err, &me),
		errors.Is(err, ErrMatchLimit), errors.Is(err, ErrDepthLimit),
		errors.Is(err, ErrHeapLimit), errors.Is(err, ErrNoMemory),
		errors.Is(err, errCalloutAbort), errors.Is(err, errCalloutBudget):
	default:
		t.Fatalf("unexpected error %T: %v", err, err)
	}
}

// checkLoc checks the offsets of one match: in range, ordered, and on
// character boundaries when the subject is UTF-8.
func checkLoc(t *testing.T, re *Regexp, s string, loc []int) {
	t.Helper()
	if loc == nil {
		return
	}
	if len(loc) != 2*(re.NumSubexp()+1) {
		t.Fatalf("got %d offsets for %d groups", len(loc), re.NumSubexp())
	}
	if loc[0] < 0 {
		t.Fatalf("match did not set group 0: %v", loc)
	}
	// \C matches a single code unit even in UTF mode, so a pattern that
	// contains it may split characters (pcre2pattern(3)).
	utf := re.cfg.flags&optUTF != 0 && utf8.ValidString(s) && !strings.Contains(re.expr, `\C`)
	for i := 0; i < len(loc); i += 2 {
		start, end := loc[i], loc[i+1]
		if start == -1 && end == -1 {
			continue
		}
		if start < 0 || start > end || end > len(s) {
			t.Fatalf("group %d at %d, %d in a subject of length %d", i/2, start, end, len(s))
		}
		if utf && (!boundary(s, start) || !boundary(s, end)) {
			t.Fatalf("group %d at %d, %d splits a character of %q", i/2, start, end, s)
		}
	}
}

func boundary(s string, i int) bool { return i == len(s) || utf8.RuneStart(s[i]) }

type fuzzResult struct {
	loc []int
	err error
}

// checkMatch runs every match method on s, checks that they agree, and
// returns the result of FindStringSubmatchIndex.
func checkMatch(t *testing.T, re *Regexp, s string) fuzzResult {
	t.Helper()
	b := []byte(s)

	loc, err := re.FindStringSubmatchIndex(s)
	checkErr(t, err)
	checkLoc(t, re, s, loc)
	res := fuzzResult{loc, err}

	if locB, errB := re.FindSubmatchIndex(b); !sameResult(loc, err, locB, errB) {
		t.Fatalf("FindStringSubmatchIndex = %v, %v; FindSubmatchIndex = %v, %v", loc, err, locB, errB)
	}
	ok, errOK := re.MatchString(s)
	if !sameErr(err, errOK) || (err == nil && ok != (loc != nil)) {
		t.Fatalf("MatchString = %v, %v; FindStringSubmatchIndex = %v, %v", ok, errOK, loc, err)
	}
	if okB, errB := re.Match(b); okB != ok || !sameErr(errOK, errB) {
		t.Fatalf("Match = %v, %v; MatchString = %v, %v", okB, errB, ok, errOK)
	}
	idx, errIdx := re.FindStringIndex(s)
	var want []int
	if loc != nil {
		want = loc[:2]
	}
	if !sameResult(idx, errIdx, want, err) {
		t.Fatalf("FindStringIndex = %v, %v; FindStringSubmatchIndex = %v, %v", idx, errIdx, loc, err)
	}
	if str, errStr := re.FindString(s); err == nil && loc != nil && str != s[loc[0]:loc[1]] || !sameErr(err, errStr) {
		t.Fatalf("FindString = %q, %v; offsets %v, %v", str, errStr, loc, err)
	}

	all, errAll := re.FindAllStringSubmatchIndex(s, -1)
	checkErr(t, errAll)
	for i, m := range all {
		checkLoc(t, re, s, m)
		if i == 0 {
			if err == nil && !reflect.DeepEqual(m, loc) {
				t.Fatalf("first of FindAll = %v, Find = %v", m, loc)
			}
			continue
		}
		prev := all[i-1]
		if m[0] < prev[1] {
			t.Fatalf("FindAll match %d at %v overlaps or precedes match %d at %v", i, m[:2], i-1, prev[:2])
		}
		if prev[0] == prev[1] && m[0] == prev[1] && m[0] == m[1] {
			t.Fatalf("FindAll repeated the empty match at %d", m[0])
		}
	}
	if err == nil && errAll == nil && (loc == nil) != (len(all) == 0) {
		t.Fatalf("Find = %v, but FindAll found %d matches", loc, len(all))
	}
	allB, errAllB := re.FindAllSubmatchIndex(b, -1)
	if !sameErr(errAll, errAllB) || !reflect.DeepEqual(all, allB) {
		t.Fatalf("FindAllStringSubmatchIndex = %v, %v; FindAllSubmatchIndex = %v, %v", all, errAll, allB, errAllB)
	}
	idxAll, errIdxAll := re.FindAllStringIndex(s, -1)
	if !sameErr(errAll, errIdxAll) || len(idxAll) != len(all) {
		t.Fatalf("FindAllStringIndex found %d (%v), FindAllStringSubmatchIndex %d (%v)", len(idxAll), errIdxAll, len(all), errAll)
	}
	for i := range idxAll {
		if !reflect.DeepEqual(idxAll[i], all[i][:2]) {
			t.Fatalf("FindAllStringIndex[%d] = %v, FindAllStringSubmatchIndex[%d] = %v", i, idxAll[i], i, all[i])
		}
	}
	return res
}

func sameResult(locA []int, errA error, locB []int, errB error) bool {
	return sameErr(errA, errB) && reflect.DeepEqual(locA, locB)
}

// sameErr reports whether two errors are the same kind of failure.
func sameErr(a, b error) bool {
	if a == nil || b == nil {
		return a == b
	}
	var ma, mb *MatchError
	if errors.As(a, &ma) && errors.As(b, &mb) {
		return ma.Code == mb.Code
	}
	return a == b
}
