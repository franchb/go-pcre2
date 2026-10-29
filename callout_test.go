package pcre2

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestCalloutFields(t *testing.T) {
	var got []Callout
	re := MustCompile(`(?C1)ab(?C"two")c`, OnCallout(func(c *Callout) error {
		cp := *c
		cp.ovec = nil
		got = append(got, cp)
		return nil
	}))
	ok, err := re.MatchString("abc")
	if !ok || err != nil {
		t.Fatalf("MatchString = %v, %v; want true, nil", ok, err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d callouts, want 2: %+v", len(got), got)
	}
	if c := got[0]; c.Number != 1 || c.String != "" || c.Position != 0 || c.StartMatch != 0 || c.Subject != "abc" {
		t.Errorf("first callout = %+v", c)
	}
	if c := got[1]; c.Number != 0 || c.String != "two" || c.Position != 2 || c.PatternPosition != 16 || c.NextItemLength != 1 {
		t.Errorf("second callout = %+v", c)
	}
}

func TestCalloutGroups(t *testing.T) {
	type group struct{ start, end int }
	var g1, g2 group
	var top, last int
	re := MustCompile(`(\d+)(x)?(?C1)-`, OnCallout(func(c *Callout) error {
		g1.start, g1.end = c.Group(1)
		g2.start, g2.end = c.Group(2)
		top, last = c.CaptureTop, c.CaptureLast
		return nil
	}))
	if ok, err := re.MatchString("12-"); !ok || err != nil {
		t.Fatalf("MatchString = %v, %v; want true, nil", ok, err)
	}
	if g1 != (group{0, 2}) || g2 != (group{-1, -1}) || top != 2 || last != 1 {
		t.Errorf("Group(1) = %v, Group(2) = %v, CaptureTop %d, CaptureLast %d; want {0 2} {-1 -1} 2 1", g1, g2, top, last)
	}
	var c Callout
	if s, e := c.Group(0); s != -1 || e != -1 {
		t.Errorf("Group(0) = %d, %d; want -1, -1", s, e)
	}
}

func TestCalloutFail(t *testing.T) {
	// Accept only even digits: Fail makes PCRE2 backtrack past odd ones.
	re := MustCompile(`(\d)(?C1)`, OnCallout(func(c *Callout) error {
		s, _ := c.Group(1)
		if (c.Subject[s]-'0')%2 != 0 {
			return Fail
		}
		return nil
	}))
	got, err := re.FindAllString("1234567", -1)
	if err != nil || !reflect.DeepEqual(got, []string{"2", "4", "6"}) {
		t.Errorf("FindAllString = %q, %v; want [2 4 6], nil", got, err)
	}
}

func TestCalloutBytesSubject(t *testing.T) {
	var subject string
	re := MustCompile(`b(?C1)`, OnCallout(func(c *Callout) error {
		subject = string([]byte(c.Subject)) // copy: it shares the caller's slice
		return nil
	}))
	if ok, err := re.Match([]byte("abc")); !ok || err != nil {
		t.Fatalf("Match = %v, %v; want true, nil", ok, err)
	}
	if subject != "abc" {
		t.Errorf("Subject = %q, want %q", subject, "abc")
	}
}

func TestCalloutAbort(t *testing.T) {
	stop := errors.New("stop")
	calls := 0
	re := MustCompile(`a(?C1)`, OnCallout(func(c *Callout) error {
		calls++
		if calls == 2 {
			return stop
		}
		return nil
	}))
	all, err := re.FindAllString("aaa", -1)
	if err != stop {
		t.Errorf("err = %v, want stop", err)
	}
	if !reflect.DeepEqual(all, []string{"a"}) {
		t.Errorf("matches before the abort = %q, want [a]", all)
	}
	// The instance is reused after an abort, and the error is not sticky.
	calls = 10
	if ok, err := re.MatchString("a"); !ok || err != nil {
		t.Errorf("MatchString after abort = %v, %v; want true, nil", ok, err)
	}
}

func TestCalloutPanic(t *testing.T) {
	boom := true
	re := MustCompile(`a(?C1)`, OnCallout(func(c *Callout) error {
		if boom {
			panic("boom")
		}
		return nil
	}))
	func() {
		defer func() {
			if v := recover(); v != "boom" {
				t.Errorf("recovered %v, want boom", v)
			}
		}()
		re.MatchString("a")
		t.Error("MatchString returned; want a panic")
	}()
	boom = false
	if ok, err := re.MatchString("a"); !ok || err != nil {
		t.Errorf("MatchString after panic = %v, %v; want true, nil", ok, err)
	}
}

func TestCalloutMark(t *testing.T) {
	var mark string
	re := MustCompile(`(*MARK:here)a(?C1)`, OnCallout(func(c *Callout) error {
		mark = c.Mark
		return nil
	}))
	if ok, err := re.MatchString("a"); !ok || err != nil {
		t.Fatalf("MatchString = %v, %v; want true, nil", ok, err)
	}
	if mark != "here" {
		t.Errorf("Mark = %q, want %q", mark, "here")
	}
}

func TestAutoCallout(t *testing.T) {
	var numbers []int
	re := MustCompile(`ab`, AutoCallout(), OnCallout(func(c *Callout) error {
		numbers = append(numbers, c.Number)
		return nil
	}))
	if ok, err := re.MatchString("ab"); !ok || err != nil {
		t.Fatalf("MatchString = %v, %v; want true, nil", ok, err)
	}
	// One before a, one before b, one at the end.
	if !reflect.DeepEqual(numbers, []int{255, 255, 255}) {
		t.Errorf("callout numbers = %v, want [255 255 255]", numbers)
	}
}

func TestCalloutIgnoredWithoutFunc(t *testing.T) {
	re := MustCompile(`a(?C1)b`)
	if ok, err := re.MatchString("ab"); !ok || err != nil {
		t.Errorf("MatchString = %v, %v; want true, nil", ok, err)
	}
}

func TestCalloutConcurrent(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	re := MustCompile(`(\w+)(?C1)@`, OnCallout(func(c *Callout) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				if loc, err := re.FindStringSubmatchIndex("user@example"); err != nil || !reflect.DeepEqual(loc, []int{0, 5, 0, 4}) {
					t.Errorf("FindStringSubmatchIndex = %v, %v", loc, err)
					return
				}
			}
		})
	}
	wg.Wait()
	if calls < 800 {
		t.Errorf("callouts = %d, want at least 800", calls)
	}
}

func TestCalloutFlags(t *testing.T) {
	if Backend != "wasm2go" {
		t.Skip("PCRE2's JIT does not report callout flags")
	}
	type flags struct{ newStart, backtracked bool }
	var got []flags
	re := MustCompile(`a(?C1)b`, OnCallout(func(c *Callout) error {
		got = append(got, flags{c.NewStart, c.Backtracked})
		return nil
	}))
	if ok, err := re.MatchString("aab"); !ok || err != nil {
		t.Fatalf("MatchString = %v, %v; want true, nil", ok, err)
	}
	// At 0 the b fails; the bumpalong to 1 is a new start after a backtrack.
	want := []flags{{true, false}, {true, true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("flags = %+v, want %+v", got, want)
	}
}
