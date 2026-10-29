package pcre2_test

import (
	"fmt"
	"strconv"

	"github.com/franchb/go-pcre2"
)

func Example() {
	// \K drops what was matched so far from the reported match.
	re := pcre2.MustCompile(`price:\s*\K\d+`)
	m, err := re.FindString("price: 42 EUR")
	if err != nil {
		panic(err)
	}
	fmt.Println(m)
	// Output: 42
}

func Example_recursion() {
	// (?R) recurses into the whole pattern: balanced parentheses.
	re := pcre2.MustCompile(`\((?:[^()]++|(?R))*\)`)
	all, err := re.FindAllString("f(a, g(b, c)) + (d)", -1)
	if err != nil {
		panic(err)
	}
	fmt.Println(all)
	// Output: [(a, g(b, c)) (d)]
}

func ExampleMatchLimit() {
	re := pcre2.MustCompile(`(a+)+$`, pcre2.MatchLimit(1000))
	_, err := re.MatchString("aaaaaaaaaaaaaaaaaaaaaaaaaaaaab")
	fmt.Println(err)
	// Output: pcre2: match limit exceeded
}

func ExampleOnCallout() {
	// The callout after each octet rejects values above 255. Fail makes
	// PCRE2 backtrack, as if a lookahead had failed, and keep looking.
	re := pcre2.MustCompile(`\b(?:(\d{1,3})(?C1)\.){3}(\d{1,3})(?C1)\b`,
		pcre2.OnCallout(func(c *pcre2.Callout) error {
			start, end := c.Group(c.CaptureLast)
			if n, _ := strconv.Atoi(c.Subject[start:end]); n > 255 {
				return pcre2.Fail
			}
			return nil
		}))
	all, err := re.FindAllString("10.0.0.1 999.1.1.1 192.168.1.300 8.8.8.8", -1)
	if err != nil {
		panic(err)
	}
	fmt.Println(all)
	// Output: [10.0.0.1 8.8.8.8]
}
