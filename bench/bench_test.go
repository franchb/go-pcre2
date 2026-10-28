// Package bench compares go-pcre2 with Go's regexp and dlclark/regexp2 on
// the same patterns and inputs. It is a separate module so that regexp2
// never appears in go-pcre2's own go.mod.
//
//	cd bench && go test -run '^$' -bench . -benchmem
//	cd bench && go test -tags pcre2_cgo -run '^$' -bench . -benchmem  # native PCRE2 + JIT
package bench

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/dlclark/regexp2/v2"
	"github.com/franchb/go-pcre2"
)

// corpus is about 64 KiB of log-like lines, deterministic across runs.
var corpus = func() string {
	var b strings.Builder
	for i := 0; b.Len() < 64<<10; i++ {
		fmt.Fprintf(&b, "%04d user%d@example.com paid $%d.%02d for item #A-%d the the order\n",
			i, i%97, i*7%1000, i%100, i%31)
	}
	return b.String()
}()

type engine struct {
	name  string
	count func(s string) (int, error)
}

func stdlib(pattern string) engine {
	re := regexp.MustCompile(pattern)
	return engine{"stdlib", func(s string) (int, error) {
		return len(re.FindAllStringIndex(s, -1)), nil
	}}
}

func re2(pattern string) engine {
	re := regexp2.MustCompile(pattern)
	return engine{"regexp2", func(s string) (int, error) {
		n := 0
		m, err := re.FindStringMatch(s)
		for m != nil && err == nil {
			n++
			m, err = re.FindNextMatch(m)
		}
		return n, err
	}}
}

func pcre(pattern string) engine {
	re := pcre2.MustCompile(pattern)
	return engine{"pcre2-" + pcre2.Backend, func(s string) (int, error) {
		all, err := re.FindAllStringIndex(s, -1)
		return len(all), err
	}}
}

var cases = []struct {
	name, pattern string
	stdlib        bool // whether Go's regexp supports the pattern
}{
	{"email", `[\w.]+@[\w.]+\.com`, true},
	{"price", `\$\d+\.\d\d`, true},
	{"lookbehind", `(?<=\$)\d+\.\d\d`, false},
	{"backreference", `\b(\w+) \1\b`, false},
	{"no-match-literal", `needle`, true},
}

func pcreNoJIT(pattern string) engine {
	re := pcre2.MustCompile(pattern, pcre2.NoJIT())
	return engine{"pcre2-" + pcre2.Backend + "-interp", func(s string) (int, error) {
		all, err := re.FindAllStringIndex(s, -1)
		return len(all), err
	}}
}

func engines(pattern string, withStdlib bool) []engine {
	es := []engine{re2(pattern), pcre(pattern)}
	if pcre2.Backend == "cgo" {
		// Native PCRE2 without its JIT: the same interpreter the wasm2go
		// build runs, so the gap to wasm2go is the cost of translation.
		es = append(es, pcreNoJIT(pattern))
	}
	if withStdlib {
		es = append([]engine{stdlib(pattern)}, es...)
	}
	return es
}

// TestSameResults checks that every engine finds the same number of
// matches, so the benchmarks compare like with like.
func TestSameResults(t *testing.T) {
	for _, c := range cases {
		var first int
		for i, e := range engines(c.pattern, c.stdlib) {
			n, err := e.count(corpus)
			if err != nil {
				t.Fatalf("%s/%s: %v", c.name, e.name, err)
			}
			if i == 0 {
				first = n
			} else if n != first {
				t.Errorf("%s: %s found %d matches, first engine found %d", c.name, e.name, n, first)
			}
		}
	}
}

func BenchmarkFindAll(b *testing.B) {
	for _, c := range cases {
		for _, e := range engines(c.pattern, c.stdlib) {
			b.Run(c.name+"/"+e.name, func(b *testing.B) {
				b.SetBytes(int64(len(corpus)))
				for b.Loop() {
					if _, err := e.count(corpus); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkSmall measures per-call overhead on a tiny subject, where the
// cost of entering the Wasm module dominates.
func BenchmarkSmall(b *testing.B) {
	const pattern, subject = `^\d{4}-\d\d-\d\d$`, "2026-10-28"
	std := regexp.MustCompile(pattern)
	r2 := regexp2.MustCompile(pattern)
	pc := pcre2.MustCompile(pattern)
	pi := pcre2.MustCompile(pattern, pcre2.NoJIT())
	b.Run("stdlib", func(b *testing.B) {
		for b.Loop() {
			if !std.MatchString(subject) {
				b.Fatal("no match")
			}
		}
	})
	b.Run("regexp2", func(b *testing.B) {
		for b.Loop() {
			if ok, err := r2.MatchString(subject); !ok || err != nil {
				b.Fatal(ok, err)
			}
		}
	})
	b.Run("pcre2-"+pcre2.Backend, func(b *testing.B) {
		for b.Loop() {
			if ok, err := pc.MatchString(subject); !ok || err != nil {
				b.Fatal(ok, err)
			}
		}
	})
	if pcre2.Backend == "cgo" {
		b.Run("pcre2-cgo-interp", func(b *testing.B) {
			for b.Loop() {
				if ok, err := pi.MatchString(subject); !ok || err != nil {
					b.Fatal(ok, err)
				}
			}
		})
	}
}
