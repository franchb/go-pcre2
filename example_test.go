package pcre2_test

import (
	"fmt"

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
