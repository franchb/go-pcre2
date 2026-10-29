//go:build pcre2_cgo

package pcre2

import (
	"encoding/binary"
	"testing"

	"github.com/franchb/go-pcre2/internal/wasm"
)

// FuzzDifferential compares the wasm2go module with native PCRE2's
// interpreter (NoJIT). Both run the same C code, so with the same inputs
// they must return the same result code and offsets; a difference is a
// bug in wasm2go's translation or in one of the two wrappers.
//
// It runs in the cgo build, which links native PCRE2 into the package,
// and drives the wasm2go module directly through internal/wasm. HeapLimit
// is left out: heap frames hold pointers and sizes, which are 4 bytes in
// Wasm and 8 natively, so the two use different amounts of heap.
func FuzzDifferential(f *testing.F) {
	if Version != wasm.PCRE2Version {
		f.Skipf("native PCRE2 is %s, the wasm2go module %s", Version, wasm.PCRE2Version)
	}
	addSeeds(f, func(s fuzzSeed) { f.Add(s.pattern, s.subject, s.flags) })
	f.Fuzz(func(t *testing.T, pattern, subject string, flags uint8) {
		t.Logf("pattern %q, subject %q, flags %#x", pattern, subject, flags)
		opts := append([]Option{MatchLimit(fuzzMatchLimit), DepthLimit(fuzzDepthLimit), NoJIT()}, fuzzFlags(flags)...)
		cfg := defaultConfig()
		for _, o := range opts {
			o(&cfg)
		}

		w := newWasmRef()
		code, errcode, erroff := w.compile(pattern, cfg.flags)
		re, err := Compile(pattern, opts...)
		if code == 0 || err != nil {
			ce, _ := err.(*CompileError)
			if code != 0 || ce == nil || ce.Code != int(errcode) || ce.Offset != erroff {
				t.Fatalf("compile: wasm2go code %d at %d (ok %v); native %v", errcode, erroff, code != 0, err)
			}
			return
		}

		want, wantErr := re.FindStringSubmatchIndex(subject)
		got, gotErr := w.match(re, code, subject)
		if !sameResult(got, gotErr, want, wantErr) {
			t.Fatalf("match %q on %q: wasm2go %v, %v; native %v, %v", pattern, subject, got, gotErr, want, wantErr)
		}
	})
}

// wasmRef is a minimal driver for the wasm2go module, independent of the
// cgo backend that the package uses in this build.
type wasmRef struct{ mod *wasm.Module }

type noCallouts struct{}

func (noCallouts) Xcallout(int32) int32 { return calloutContinue }

func newWasmRef() *wasmRef {
	mod := wasm.New(noCallouts{})
	mod.X_initialize()
	return &wasmRef{mod}
}

func (w *wasmRef) mem() []byte { return *w.mod.Xmemory().Slice() }

func (w *wasmRef) put(b []byte) int32 {
	p := w.mod.Xmalloc(int32(len(b) + 1))
	copy(w.mem()[p:], b)
	return p
}

func (w *wasmRef) u32(p int32) uint32 { return binary.LittleEndian.Uint32(w.mem()[p:]) }

func (w *wasmRef) compile(pattern string, flags uint32) (code, errcode int32, erroff int) {
	pat := w.put([]byte(pattern))
	out := w.mod.Xmalloc(8)
	code = w.mod.Xpcre2_compile_8(pat, int32(len(pattern)), int32(flags), out, out+4, 0)
	return code, int32(w.u32(out)), int(w.u32(out + 4))
}

// match runs one pcre2_match from offset 0 with the same options and
// limits the package would use for re, and maps the result as find does.
func (w *wasmRef) match(re *Regexp, code int32, subject string) ([]int, error) {
	m := w.mod
	md := m.Xpcre2_match_data_create_from_pattern_8(code, 0)
	mctx := m.Xpcre2_match_context_create_8(0)
	m.Xpcre2_set_match_limit_8(mctx, fuzzMatchLimit)
	m.Xpcre2_set_depth_limit_8(mctx, fuzzDepthLimit)
	subj := w.put([]byte(subject))
	rc := m.Xpcre2_match_8(code, subj, int32(len(subject)), 0, int32(utfCheck(re, subject)), md, mctx)

	switch {
	case rc == errNoMatch:
		return nil, nil
	case rc < 0:
		return nil, wasmMatchErr(rc)
	}
	npair := int(uint32(m.Xpcre2_get_ovector_count_8(md)))
	if rc == 0 {
		rc = int32(npair)
	}
	ovec := m.Xpcre2_get_ovector_pointer_8(md)
	loc := make([]int, 2*npair)
	for i := range loc {
		loc[i] = offset(uint64(w.u32(ovec+int32(4*i))), uint64(unset32), i, int(rc))
	}
	return loc, nil
}

const unset32 = 0xffffffff

func wasmMatchErr(rc int32) error {
	switch rc {
	case errMatchLimit:
		return ErrMatchLimit
	case errDepthLimit:
		return ErrDepthLimit
	case errHeapLimit:
		return ErrHeapLimit
	case errNoMemory:
		return ErrNoMemory
	}
	return &MatchError{Code: int(rc)}
}
