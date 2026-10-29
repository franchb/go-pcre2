//go:build pcre2_cgo

package pcre2

/*
#cgo pkg-config: libpcre2-8
#define PCRE2_CODE_UNIT_WIDTH 8
#include <stdint.h>
#include <stdlib.h>
#include <pcre2.h>

// Thin wrappers over the 8-bit API. pcre2.h maps the generic names to
// these _8 functions with macros, which cgo cannot call.

static pcre2_code_8 *gp_compile(const uint8_t *pat, size_t len, uint32_t opts, int *errcode, size_t *erroff) {
	return pcre2_compile_8(pat, len, opts, errcode, erroff, NULL);
}

static int gp_jit(pcre2_code_8 *code) {
	return pcre2_jit_compile_8(code, PCRE2_JIT_COMPLETE);
}

static int gp_match(const pcre2_code_8 *code, const uint8_t *subj, size_t len, size_t start,
		uint32_t opts, pcre2_match_data_8 *md, pcre2_match_context_8 *mctx) {
	return pcre2_match_8(code, subj, len, start, opts, md, mctx);
}

static int gp_info_u32(const pcre2_code_8 *code, uint32_t what, uint32_t *out) {
	return pcre2_pattern_info_8(code, what, out);
}

static const uint8_t *gp_name_table(const pcre2_code_8 *code) {
	PCRE2_SPTR8 table = NULL;
	pcre2_pattern_info_8(code, PCRE2_INFO_NAMETABLE, &table);
	return table;
}

static int gp_version(uint8_t *buf) {
	return pcre2_config_8(PCRE2_CONFIG_VERSION, buf);
}

static uint32_t gp_has_jit(void) {
	uint32_t jit = 0;
	pcre2_config_8(PCRE2_CONFIG_JIT, &jit);
	return jit;
}

// Callouts go to goCallout (callout_cgo.go) with the cgo.Handle of the
// instance's callouts as data.
extern int goCallout(pcre2_callout_block_8 *block, uintptr_t handle);

static int gp_callout(pcre2_callout_block_8 *block, void *data) {
	return goCallout(block, (uintptr_t)data);
}

static int gp_set_callout(pcre2_match_context_8 *mctx, uintptr_t handle) {
	return pcre2_set_callout_8(mctx, gp_callout, (void *)handle);
}

static void gp_free(pcre2_code_8 *code, pcre2_match_data_8 *md, pcre2_match_context_8 *mctx) {
	pcre2_match_context_free_8(mctx);
	pcre2_match_data_free_8(md);
	pcre2_code_free_8(code);
}
*/
import "C"

import (
	"runtime"
	"runtime/cgo"
	"strings"
	"unsafe"
)

// Version is the PCRE2 release this package is linked against.
var Version = func() string {
	var buf [64]byte
	n := C.gp_version((*C.uint8_t)(&buf[0]))
	if n <= 1 {
		return ""
	}
	v, _, _ := strings.Cut(string(buf[:n-1]), " ") // "10.49 2026-09-28"
	return v
}()

// Backend names the PCRE2 implementation in use: "wasm2go" by default,
// "cgo" when built with -tags pcre2_cgo.
const Backend = "cgo"

// JIT reports whether the linked PCRE2 has the JIT compiler. Patterns are
// JIT-compiled when it does; a pattern the JIT rejects falls back to the
// interpreter.
var JIT = C.gp_has_jit() != 0

var emptySubject [1]byte

// instance is one compiled copy of the pattern with its match data. The
// compiled code is read-only and could be shared, but one copy per pooled
// instance keeps this backend shaped like the wasm2go one.
type instance struct {
	code  *C.pcre2_code_8
	md    *C.pcre2_match_data_8
	mctx  *C.pcre2_match_context_8
	ovec  *C.size_t
	npair int
	subj  *C.uint8_t // the current subject, in Go memory, during a call

	co *callouts // nil unless the pattern has a CalloutFunc
}

type handles struct {
	code *C.pcre2_code_8
	md   *C.pcre2_match_data_8
	mctx *C.pcre2_match_context_8
	co   cgo.Handle // 0 if none
}

func (re *Regexp) newInstance() (*instance, error) {
	pat := C.CString(re.expr)
	defer C.free(unsafe.Pointer(pat))

	var errcode C.int
	var erroff C.size_t
	code := C.gp_compile((*C.uint8_t)(unsafe.Pointer(pat)), C.size_t(len(re.expr)), C.uint32_t(re.cfg.flags), &errcode, &erroff)
	if code == nil {
		return nil, &CompileError{Pattern: re.expr, Offset: int(erroff), Code: int(errcode), Message: errorMessage(int32(errcode))}
	}
	if !re.cfg.noJIT {
		C.gp_jit(code) // non-zero means the JIT declined; the interpreter is used
	}

	in := &instance{code: code}
	in.md = C.pcre2_match_data_create_from_pattern_8(code, nil)
	if in.md == nil {
		C.gp_free(code, nil, nil)
		return nil, ErrNoMemory
	}
	in.ovec = C.pcre2_get_ovector_pointer_8(in.md)
	in.npair = int(C.pcre2_get_ovector_count_8(in.md))

	var co cgo.Handle
	if c := re.cfg; c.matchLimit != 0 || c.depthLimit != 0 || c.heapLimit != 0 || c.callout != nil {
		in.mctx = C.pcre2_match_context_create_8(nil)
		if in.mctx == nil {
			C.gp_free(code, in.md, nil)
			return nil, ErrNoMemory
		}
		if c.callout != nil {
			// The handle refers to the callouts, not the instance, so it
			// does not keep the instance from being collected.
			in.co = &callouts{fn: c.callout}
			co = cgo.NewHandle(in.co)
			C.gp_set_callout(in.mctx, C.uintptr_t(co))
		}
		if c.matchLimit != 0 {
			C.pcre2_set_match_limit_8(in.mctx, C.uint32_t(c.matchLimit))
		}
		if c.depthLimit != 0 {
			C.pcre2_set_depth_limit_8(in.mctx, C.uint32_t(c.depthLimit))
		}
		if c.heapLimit != 0 {
			C.pcre2_set_heap_limit_8(in.mctx, C.uint32_t(c.heapLimit))
		}
	}

	runtime.AddCleanup(in, func(h handles) {
		C.gp_free(h.code, h.md, h.mctx)
		if h.co != 0 {
			h.co.Delete()
		}
	}, handles{in.code, in.md, in.mctx, co})
	return in, nil
}

// load points the instance at the subject. Nothing is copied: the subject
// stays in Go memory, which cgo allows because it holds no Go pointers.
func load[T subject](in *instance, s T) {
	if len(s) == 0 {
		in.subj = (*C.uint8_t)(unsafe.Pointer(&emptySubject[0]))
		return
	}
	var p *byte
	if b, ok := any(s).([]byte); ok {
		p = unsafe.SliceData(b)
	} else {
		str := string(s)
		p = unsafe.StringData(str)
	}
	in.subj = (*C.uint8_t)(unsafe.Pointer(p))
	if in.co != nil {
		in.co.subject = subjectString(s)
	}
}

// unload drops the references to the last subject before pooling.
func (in *instance) unload() {
	in.subj = nil
	if in.co != nil {
		in.co.subject = ""
	}
}

func errorMessage(code int32) string {
	var buf [256]byte
	n := C.pcre2_get_error_message_8(C.int(code), (*C.uint8_t)(&buf[0]), C.size_t(len(buf)))
	if n < 0 {
		return "unknown error"
	}
	return string(buf[:n])
}

func (in *instance) errorMessage(code int32) string { return errorMessage(code) }

func (in *instance) exec(n, start int, opts uint32) (int, error) {
	rc := C.gp_match(in.code, in.subj, C.size_t(n), C.size_t(start), C.uint32_t(opts), in.md, in.mctx)
	return matchResult(in, int32(rc))
}

func (in *instance) offsets(rc int, loc []int) {
	for i, v := range unsafe.Slice(in.ovec, len(loc)) {
		loc[i] = offset(uint64(v), uint64(^C.size_t(0)), i, rc)
	}
}

func (in *instance) groupNames() []string {
	var count, size, captures C.uint32_t
	C.gp_info_u32(in.code, infoCaptureCount, &captures)
	C.gp_info_u32(in.code, infoNameCount, &count)
	C.gp_info_u32(in.code, infoNameEntrySize, &size)

	names := make([]string, int(captures)+1)
	if count == 0 {
		return names
	}
	table := unsafe.Slice((*byte)(unsafe.Pointer(C.gp_name_table(in.code))), int(count)*int(size))
	for i := 0; i < int(count); i++ {
		e := table[i*int(size) : (i+1)*int(size)]
		group := int(e[0])<<8 | int(e[1])
		name := e[2:]
		for j, c := range name {
			if c == 0 {
				name = name[:j]
				break
			}
		}
		if names[group] == "" {
			names[group] = string(name)
		}
	}
	return names
}
