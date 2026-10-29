// Command fuzzseeds extracts fuzz seeds from PCRE2's own test data.
//
//	go run ./buildtools/fuzzseeds PCRE2_SOURCE_DIR > testdata/pcre2-seeds.txt
//
// It reads testdata/testinput* in pcre2test's format and keeps the
// single-line /pattern/modifiers patterns with up to three subject lines
// each. Patterns go through unchanged, since PCRE2 parses their escapes
// itself. Subject lines get pcre2test's escape processing (\n, \t, \xhh,
// \x{h..}, octal and the like), with any \= modifiers removed. The output
// format is described at pcre2Seeds in fuzz_test.go.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxSubjects = 3
	maxLen      = 1024
)

// Flag bits, as in fuzz_test.go.
const (
	fuzzCaseless = 1 << iota
	fuzzDotAll
	fuzzExtended
	fuzzMultiline
	fuzzUCP
)

// A single-line pattern: /.../ followed by comma-separated modifiers.
var patternLine = regexp.MustCompile(`^/(.*)/([A-Za-z0-9_,=\-+ ]*)$`)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fuzzseeds PCRE2_SOURCE_DIR")
		os.Exit(2)
	}
	files, err := filepath.Glob(filepath.Join(os.Args[1], "testdata", "testinput*"))
	if err != nil || len(files) == 0 {
		fmt.Fprintln(os.Stderr, "fuzzseeds: no testdata/testinput* files", err)
		os.Exit(1)
	}
	sort.Strings(files)

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	fmt.Fprintln(w, "# Fuzz seeds extracted by buildtools/fuzzseeds from PCRE2's testdata/testinput*.")
	fmt.Fprintln(w, "# PCRE2 is BSD-3-Clause WITH PCRE2-exception; see LICENSES/PCRE2-LICENCE.md.")
	seen := map[string]bool{}
	for _, name := range files {
		if err := extract(w, name, seen); err != nil {
			fmt.Fprintln(os.Stderr, "fuzzseeds:", err)
			os.Exit(1)
		}
	}
}

func extract(w *bufio.Writer, name string, seen map[string]bool) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	subjects := -1 // subjects left for the current pattern; -1 outside one
	for sc.Scan() {
		line := sc.Text()
		if m := patternLine.FindStringSubmatch(line); m != nil {
			subjects = -1
			pattern, flags := m[1], modifierFlags(m[2])
			key := fmt.Sprint(flags, pattern)
			if len(pattern) > maxLen || !utf8.ValidString(pattern) || seen[key] {
				continue
			}
			seen[key] = true
			fmt.Fprintf(w, "/%d %s\n", flags, strconv.Quote(pattern))
			subjects = maxSubjects
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case trimmed == "", strings.HasPrefix(line, "/"), strings.HasPrefix(line, "#"):
			subjects = -1 // a blank line, comment or unparsed pattern ends the block
		case subjects > 0 && trimmed != line && !strings.HasPrefix(trimmed, `\=`):
			s := unescape(trimmed)
			if len(s) <= maxLen {
				fmt.Fprintf(w, "\t%s\n", strconv.Quote(s))
				subjects--
			}
		}
	}
	return sc.Err()
}

func modifierFlags(mods string) uint8 {
	var flags uint8
	for _, m := range strings.Split(mods, ",") {
		m = strings.TrimSpace(m)
		if m == "ucp" {
			flags |= fuzzUCP
			continue
		}
		if strings.ContainsAny(m, "=_") || len(m) > 6 {
			continue // a named modifier with a value, or a long one
		}
		for _, c := range m {
			switch c {
			case 'i':
				flags |= fuzzCaseless
			case 's':
				flags |= fuzzDotAll
			case 'x':
				flags |= fuzzExtended
			case 'm':
				flags |= fuzzMultiline
			}
		}
	}
	return flags
}

// unescape applies the common part of pcre2test's subject escapes and
// drops a trailing \= modifier list.
func unescape(s string) string {
	if i := strings.Index(s, `\=`); i >= 0 {
		s = s[:i]
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 == len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch c = s[i]; c {
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'e':
			b.WriteByte(0x1b)
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case 'x':
			if i+1 < len(s) && s[i+1] == '{' {
				end := strings.IndexByte(s[i:], '}')
				if end > 0 {
					if v, err := strconv.ParseUint(s[i+2:i+end], 16, 32); err == nil {
						if v < 0x100 && !utf8.ValidRune(rune(v)) || v > utf8.MaxRune {
							b.WriteByte(byte(v))
						} else {
							b.WriteRune(rune(v))
						}
						i += end
						continue
					}
				}
			}
			j := i + 1
			for j < len(s) && j < i+3 && isHex(s[j]) {
				j++
			}
			if v, err := strconv.ParseUint(s[i+1:j], 16, 8); err == nil {
				b.WriteByte(byte(v))
			}
			i = j - 1
		case '0', '1', '2', '3', '4', '5', '6', '7':
			j := i
			for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
				j++
			}
			v, _ := strconv.ParseUint(s[i:j], 8, 16)
			b.WriteByte(byte(v))
			i = j - 1
		default:
			b.WriteByte(c) // \\, \$ and other escaped literals
		}
	}
	return b.String()
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}
