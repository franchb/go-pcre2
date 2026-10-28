//go:build !pcre2_cgo

package pcre2

import (
	"encoding/binary"

	"github.com/franchb/go-pcre2/internal/wasm"
)

// Version is the PCRE2 release this package was built from.
const Version = wasm.PCRE2Version

// Backend names the PCRE2 implementation in use: "wasm2go" by default,
// "cgo" when built with -tags pcre2_cgo.
const Backend = "wasm2go"

const unset = 0xffffffff // PCRE2_UNSET in a 32-bit PCRE2_SIZE

// instance is one WebAssembly module with the pattern compiled into it.
// It is not safe for concurrent use.
type instance struct {
	mod   *wasm.Module
	code  int32 // pcre2_code *
	md    int32 // pcre2_match_data *
	mctx  int32 // pcre2_match_context *, or 0
	ovec  int32 // PCRE2_SIZE *, owned by md
	npair int

	subj    int32 // subject buffer
	subjCap int32
}

func (re *Regexp) newInstance() (in *instance, err error) {
	defer func() {
		if v := recover(); v != nil {
			in, err = nil, trapError(v)
		}
	}()

	mod := wasm.New()
	mod.X_initialize()
	in = &instance{mod: mod}

	// pcre2_compile copies the pattern, so the buffer is freed right after.
	pat := in.alloc(len(re.expr) + 8)
	errcode, erroff := pat+int32(len(re.expr)), pat+int32(len(re.expr))+4
	copy(in.mem()[pat:], re.expr)
	in.code = mod.Xpcre2_compile_8(pat, int32(len(re.expr)), int32(re.cfg.flags), errcode, erroff, 0)
	if in.code == 0 {
		code := int32(binary.LittleEndian.Uint32(in.mem()[errcode:]))
		off := int(binary.LittleEndian.Uint32(in.mem()[erroff:]))
		mod.Xfree(pat)
		return nil, &CompileError{Pattern: re.expr, Offset: off, Code: int(code), Message: in.errorMessage(code)}
	}
	mod.Xfree(pat)

	in.md = mod.Xpcre2_match_data_create_from_pattern_8(in.code, 0)
	if in.md == 0 {
		return nil, ErrNoMemory
	}
	in.ovec = mod.Xpcre2_get_ovector_pointer_8(in.md)
	in.npair = int(uint32(mod.Xpcre2_get_ovector_count_8(in.md)))

	if c := re.cfg; c.matchLimit != 0 || c.depthLimit != 0 || c.heapLimit != 0 {
		in.mctx = mod.Xpcre2_match_context_create_8(0)
		if in.mctx == 0 {
			return nil, ErrNoMemory
		}
		if c.matchLimit != 0 {
			mod.Xpcre2_set_match_limit_8(in.mctx, int32(c.matchLimit))
		}
		if c.depthLimit != 0 {
			mod.Xpcre2_set_depth_limit_8(in.mctx, int32(c.depthLimit))
		}
		if c.heapLimit != 0 {
			mod.Xpcre2_set_heap_limit_8(in.mctx, int32(c.heapLimit))
		}
	}
	return in, nil
}

// mem returns the module's linear memory. Any call into the module may grow
// and move it, so callers must not keep the slice across calls.
func (in *instance) mem() []byte { return *in.mod.Xmemory().Slice() }

func (in *instance) alloc(n int) int32 {
	p := in.mod.Xmalloc(int32(n))
	if p == 0 {
		panic(ErrNoMemory)
	}
	return p
}

// load copies the subject into the module, growing the buffer if needed.
func load[T subject](in *instance, s T) {
	if in.subj == 0 || int(in.subjCap) < len(s) {
		if in.subj != 0 {
			in.mod.Xfree(in.subj)
			in.subj = 0
		}
		size := max(len(s), 64)
		in.subj, in.subjCap = in.alloc(size), int32(size)
	}
	copy(in.mem()[in.subj:], s)
}

// unload is a no-op: the subject was copied into linear memory.
func (in *instance) unload() {}

func (in *instance) errorMessage(code int32) string {
	const size = 256
	buf := in.alloc(size)
	defer in.mod.Xfree(buf)
	n := in.mod.Xpcre2_get_error_message_8(code, buf, size)
	if n < 0 {
		return "unknown error"
	}
	return string(in.mem()[buf : buf+n])
}

// match runs pcre2_match on the loaded subject of length n.
func (in *instance) match(n, start int, opts uint32) ([]int, error) {
	rc := in.mod.Xpcre2_match_8(in.code, in.subj, int32(n), int32(start), int32(opts), in.md, in.mctx)
	if err := matchErr(in, rc); err != nil || rc == errNoMatch {
		return nil, err
	}
	if rc == 0 {
		// The ovector was too small. It never is, since md is sized from
		// the pattern, but treat every pair as valid if it happens.
		rc = int32(in.npair)
	}

	mem := in.mem()
	loc := make([]int, 2*in.npair)
	for i := range loc {
		v := binary.LittleEndian.Uint32(mem[in.ovec+int32(4*i):])
		if v == unset {
			loc[i] = -1
		} else {
			loc[i] = int(v)
		}
	}
	// Pairs past rc did not take part in the match.
	for i := 2 * int(rc); i < len(loc); i++ {
		loc[i] = -1
	}
	return loc, nil
}

// groupNames reads the capture count and name table of the pattern.
func (in *instance) groupNames() []string {
	out := in.alloc(4)
	defer in.mod.Xfree(out)
	info := func(what int32) uint32 {
		in.mod.Xpcre2_pattern_info_8(in.code, what, out)
		return binary.LittleEndian.Uint32(in.mem()[out:])
	}

	names := make([]string, info(infoCaptureCount)+1)
	count, size := info(infoNameCount), info(infoNameEntrySize)
	if count == 0 {
		return names
	}
	table := info(infoNameTable)
	mem := in.mem()
	for i := range count {
		e := mem[table+i*size : table+(i+1)*size]
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
