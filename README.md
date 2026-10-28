# go-pcre2

PCRE2 for Go without cgo. PCRE2 is compiled to WebAssembly with wasi-sdk and translated to plain Go with [wasm2go](https://github.com/ncruces/wasm2go). There is no Wasm runtime at run time, and `CGO_ENABLED=0` builds and cross-compiles work like any other Go package.

> Status: experimental. The API may change before v1. Benchmarks are produced by CI and are not published here until they have been reviewed.

```go
import "github.com/franchb/go-pcre2"

re := pcre2.MustCompile(`\((?:[^()]++|(?R))*\)`) // balanced parentheses
all, err := re.FindAllString("f(a, g(b, c)) + (d)", -1)
// all == ["(a, g(b, c))", "(d)"]
```

## Why

Go's `regexp` is RE2: linear-time and safe, but with no lookaround or backreferences. [dlclark/regexp2](https://github.com/dlclark/regexp2) covers most PCRE-style patterns in pure Go, but not `\K`, recursion `(?R)`, subroutine calls `(?1)`/`(?&name)` or branch reset `(?|...)`. If your patterns need those and you can't use cgo, this package gives you PCRE2 itself. Callouts are not supported yet; see the roadmap.

If your patterns work in `regexp` or `regexp2`, use those. They are faster to call and simpler to audit.

## How it is built

`make generate` runs [`buildtools/wasm/Containerfile`](buildtools/wasm/Containerfile) with podman (or `CONTAINER=docker`):

1. Clone PCRE2 at the tag in [`versions.env`](buildtools/wasm/versions.env) and check that the tag still points at the pinned commit.
2. Build the 8-bit library with wasi-sdk, JIT off. PCRE2's JIT has no WebAssembly backend.
3. Link a reactor module that exports only the functions in [`exports.txt`](buildtools/wasm/exports.txt) and its memory.
4. Fail the build if the module imports anything. With no imports, the module cannot reach files, clocks, the network or the process, whatever the pattern or subject.
5. Translate the module to Go with the pinned wasm2go version, and record the inputs and the `.wasm` SHA-256 in `internal/wasm/versions.go`.

CI regenerates on every run and fails if the result differs from what is committed.

## Behaviour

- Offsets are byte offsets, as in `regexp`.
- Patterns and subjects are UTF-8 by default. Invalid UTF-8 in a subject fails to match rather than returning an error (`PCRE2_MATCH_INVALID_UTF`). Use `Bytes()` to match raw bytes.
- `\d`, `\w`, `\s` are ASCII unless `UCP()` is set, as in `regexp`.
- Match methods return an `error`, because a backtracking match can hit a limit.
- `FindAll*` follows Perl after an empty match: the next attempt at the same position must be non-empty, and if that fails the search moves on one character. Go's `regexp` instead never reports an empty match right after a previous match, so `a*` on `"baaab"` gives four matches here and three there.

## Hostile input

PCRE2 is a backtracking engine. A bad pattern on the wrong input can take exponential time, whatever the runtime. On untrusted input, set limits and handle the errors:

```go
re := pcre2.MustCompile(pattern, pcre2.MatchLimit(100_000), pcre2.HeapLimit(16<<10))
ok, err := re.MatchString(subject)
if errors.Is(err, pcre2.ErrMatchLimit) { /* treat as a failed rule */ }
```

PCRE2's defaults are a match limit of 10,000,000 and a heap limit of 20,000,000 KiB, which is more than a 32-bit Wasm memory can hold.

## Concurrency and memory

A `Regexp` is safe for concurrent use. Each goroutine matching at the same time gets its own Wasm instance with its own copy of the compiled pattern, taken from a `sync.Pool`. Instances are ordinary Go memory, so there is no `Close`. Memory per `Regexp` therefore grows with the number of concurrent matchers.

## Versions

Each release is built from one PCRE2 release; `pcre2.Version` reports which. The plan is to release alongside every PCRE2 version, with the bump PR opened automatically.

## Roadmap

- A cgo backend behind a build tag, linking the system PCRE2 with the JIT, so the same benchmarks can compare against native PCRE2.
- A scheduled workflow that opens a PR when PCRE2 tags a release.
- Callouts, which need a host import and so a deliberate decision about the no-imports rule.

## License

BSD-3-Clause; see [LICENSE](LICENSE). The generated code includes PCRE2 (BSD-3-Clause WITH PCRE2-exception) and wasi-libc; see [NOTICE](NOTICE).
