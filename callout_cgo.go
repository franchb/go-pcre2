//go:build pcre2_cgo

package pcre2

/*
#define PCRE2_CODE_UNIT_WIDTH 8
#include <stdint.h>
#include <pcre2.h>
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"
)

// goCallout is the callout function of patterns with a CalloutFunc; see
// gp_callout in backend_cgo.go. This file's preamble may only declare,
// because it exports a function.
//
//export goCallout
func goCallout(b *C.pcre2_callout_block_8, h C.uintptr_t) C.int {
	co := cgo.Handle(h).Value().(*callouts)
	c := &co.c
	c.Number = int(b.callout_number)
	c.CaptureTop = int(b.capture_top)
	c.CaptureLast = int(b.capture_last)
	c.StartMatch = int(b.start_match)
	c.Position = int(b.current_position)
	c.PatternPosition = int(b.pattern_position)
	c.NextItemLength = int(b.next_item_length)
	c.NewStart = b.callout_flags&calloutStartMatch != 0
	c.Backtracked = b.callout_flags&calloutBacktrack != 0
	c.Subject = co.subject

	c.String = ""
	if b.callout_string != nil {
		c.String = co.str(int(b.callout_string_offset), func() string {
			return C.GoStringN((*C.char)(unsafe.Pointer(b.callout_string)), C.int(b.callout_string_length))
		})
	}
	c.Mark = ""
	if b.mark != nil {
		c.Mark = C.GoString((*C.char)(unsafe.Pointer(b.mark)))
	}
	c.ovec = c.ovec[:0]
	for _, v := range unsafe.Slice(b.offset_vector, 2*c.CaptureTop) {
		off := -1
		if v != ^C.size_t(0) {
			off = int(v)
		}
		c.ovec = append(c.ovec, off)
	}
	return C.int(co.call())
}
