package pcre2

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestVersion(t *testing.T) {
	if Version != "10.49" {
		t.Errorf("Version = %q, want 10.49", Version)
	}
}

// Features PCRE2 has and Go's regexp and dlclark/regexp2 don't.
func TestPCRE2OnlyFeatures(t *testing.T) {
	tests := []struct {
		name, pattern, subject string
		want                   []int
	}{
		{"reset match start", `foo\Kbar`, "xfoobar", []int{4, 7}},
		{"recursion", `\((?:[^()]++|(?R))*\)`, "x(a(b)c)y", []int{1, 8}},
		{"subroutine call", `(?<pair>\d\d)-(?&pair)`, "ab 12-34", []int{3, 8}},
		{"numbered subroutine", `(\d\d)-(?1)`, "ab 12-34", []int{3, 8}},
		{"branch reset", `(?|(a)|(b))\1`, "xbb", []int{1, 3}},
		{"possessive", `a++b`, "aaab", []int{0, 4}},
		{"lookbehind", `(?<=\$)\d+`, "cost $42", []int{6, 8}},
		{"unicode property", `\p{Greek}+`, "abc αβγ", []int{4, 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := MustCompile(tt.pattern)
			got, err := re.FindStringIndex(tt.subject)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindStringIndex(%q) = %v, want %v", tt.subject, got, tt.want)
			}
		})
	}
}

func TestNoMatch(t *testing.T) {
	re := MustCompile(`a++a`) // possessive: never gives an a back
	ok, err := re.MatchString("aaaa")
	if err != nil || ok {
		t.Errorf("MatchString = %v, %v; want false, nil", ok, err)
	}
	loc, err := re.FindStringIndex("aaaa")
	if err != nil || loc != nil {
		t.Errorf("FindStringIndex = %v, %v; want nil, nil", loc, err)
	}
}

func TestSubmatches(t *testing.T) {
	re := MustCompile(`(?<year>\d{4})-(?<month>\d\d)(?:-(?<day>\d\d))?`)
	if n := re.NumSubexp(); n != 3 {
		t.Errorf("NumSubexp = %d, want 3", n)
	}
	if got, want := re.SubexpNames(), []string{"", "year", "month", "day"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SubexpNames = %q, want %q", got, want)
	}
	if i := re.SubexpIndex("month"); i != 2 {
		t.Errorf("SubexpIndex(month) = %d, want 2", i)
	}
	if i := re.SubexpIndex("week"); i != -1 {
		t.Errorf("SubexpIndex(week) = %d, want -1", i)
	}

	got, err := re.FindStringSubmatch("on 2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"2026-10", "2026", "10", ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("FindStringSubmatch = %q, want %q", got, want)
	}
	loc, err := re.FindStringSubmatchIndex("on 2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{3, 10, 3, 7, 8, 10, -1, -1}; !reflect.DeepEqual(loc, want) {
		t.Errorf("FindStringSubmatchIndex = %v, want %v", loc, want)
	}
}

func TestBytesSubject(t *testing.T) {
	re := MustCompile(`b+`)
	loc, err := re.FindSubmatchIndex([]byte("abbc"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 3}; !reflect.DeepEqual(loc, want) {
		t.Errorf("FindSubmatchIndex = %v, want %v", loc, want)
	}
}

func TestCompileError(t *testing.T) {
	_, err := Compile(`a(b`)
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("Compile error = %v, want *CompileError", err)
	}
	if ce.Offset != 3 {
		t.Errorf("Offset = %d, want 3", ce.Offset)
	}
	if !strings.Contains(ce.Message, "missing closing parenthesis") {
		t.Errorf("Message = %q", ce.Message)
	}
}

func TestMatchLimit(t *testing.T) {
	re := MustCompile(`(a+)+$`, MatchLimit(1000))
	_, err := re.MatchString(strings.Repeat("a", 30) + "b")
	if !errors.Is(err, ErrMatchLimit) {
		t.Errorf("err = %v, want ErrMatchLimit", err)
	}
	// The instance stays usable after a limit error.
	ok, err := re.MatchString("aaa")
	if err != nil || !ok {
		t.Errorf("MatchString after limit = %v, %v; want true, nil", ok, err)
	}
}

func TestFindAllEmptyMatches(t *testing.T) {
	tests := []struct {
		pattern, subject string
		want             [][]int
	}{
		// Perl semantics: an empty match is allowed right after a
		// non-empty one, but not twice at the same position.
		{`a*`, "baaab", [][]int{{0, 0}, {1, 4}, {4, 4}, {5, 5}}},
		// Advancing after an empty match moves by a whole UTF-8 character.
		{``, "αb", [][]int{{0, 0}, {2, 2}, {3, 3}}},
		{`\d+`, "a1b22c333", [][]int{{1, 2}, {3, 5}, {6, 9}}},
	}
	for _, tt := range tests {
		re := MustCompile(tt.pattern)
		got, err := re.FindAllStringIndex(tt.subject, -1)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("FindAllStringIndex(%q, %q) = %v, want %v", tt.pattern, tt.subject, got, tt.want)
		}
	}

	re := MustCompile(`\d+`)
	got, err := re.FindAllString("a1b22c333", 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1", "22"}; !reflect.DeepEqual(got, want) {
		t.Errorf("FindAllString(n=2) = %q, want %q", got, want)
	}
}

func TestUTF(t *testing.T) {
	// Invalid UTF-8 in the subject fails to match instead of erroring.
	loc, err := MustCompile(`abc`).FindStringIndex("\xffabc")
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 4}; !reflect.DeepEqual(loc, want) {
		t.Errorf("invalid UTF subject: %v, want %v", loc, want)
	}

	// Bytes mode: \xff is the single byte 0xff.
	loc, err = MustCompile(`\xff`, Bytes()).FindStringIndex("a\xffb")
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(loc, want) {
		t.Errorf("Bytes: %v, want %v", loc, want)
	}

	// \w is ASCII unless UCP is set, as in Go's regexp.
	ascii, _ := MustCompile(`\w+`).FindString("héllo")
	uni, _ := MustCompile(`\w+`, UCP()).FindString("héllo")
	if ascii != "h" || uni != "héllo" {
		t.Errorf(`\w+ = %q without UCP, %q with; want "h", "héllo"`, ascii, uni)
	}

	ok, err := MustCompile(`ÄÖ`, Caseless()).MatchString("äö")
	if err != nil || !ok {
		t.Errorf("Caseless Unicode = %v, %v; want true, nil", ok, err)
	}
}

func TestLargeSubject(t *testing.T) {
	s := strings.Repeat("x", 1<<20) + "needle"
	loc, err := MustCompile(`needle`).FindStringIndex(s)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1 << 20, 1<<20 + 6}; !reflect.DeepEqual(loc, want) {
		t.Errorf("FindStringIndex = %v, want %v", loc, want)
	}
}

func TestConcurrent(t *testing.T) {
	re := MustCompile(`(\w+)@(\w+)\.com`)
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Go(func() {
			for i := range 100 {
				user := strings.Repeat("u", g+1)
				domain := strings.Repeat("d", i%7+1)
				m, err := re.FindStringSubmatch("mail " + user + "@" + domain + ".com now")
				if err != nil {
					t.Error(err)
					return
				}
				if len(m) != 3 || m[1] != user || m[2] != domain {
					t.Errorf("got %q", m)
					return
				}
			}
		})
	}
	wg.Wait()
}
