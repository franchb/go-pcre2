# go-pcre2

PCRE2 for Go without cgo. PCRE2 is compiled to WebAssembly with wasi-sdk and translated to plain Go with [wasm2go](https://github.com/ncruces/wasm2go). There is no Wasm runtime at run time, and `CGO_ENABLED=0` builds and cross-compiles work like any other Go package.

> Status: experimental; the API may change before v1. Current release: `v0.49.0`, built from PCRE2 10.49.

```go
import "github.com/franchb/go-pcre2"

re := pcre2.MustCompile(`\((?:[^()]++|(?R))*\)`) // balanced parentheses
all, err := re.FindAllString("f(a, g(b, c)) + (d)", -1)
// all == ["(a, g(b, c))", "(d)"]
```

**Background.** This library exists because of a blog post: [PCRE in Go in 2026: wasm2go Is Ready, PCRE2 Isn't](https://franchb.com/posts/pcre-regex-in-go-2026/). The post ended by asking for "a maintained go-pcre2 on wasm2go, released alongside each PCRE2 version, with honest benchmarks against a native JIT build". Its [update](https://franchb.com/posts/pcre-regex-in-go-2026/#update-so-i-built-it) tells how that got built in an evening, including the performance bug I wrote first.

## Contents

- [Install](#install)
- [When to use it](#when-to-use-it)
- [API](#api)
- [Behaviour](#behaviour)
- [Hostile input](#hostile-input)
- [Concurrency and memory](#concurrency-and-memory)
- [Performance](#performance)
- [Security model](#security-model)
- [How it is built](#how-it-is-built)
- [cgo backend](#cgo-backend)
- [Versions and releases](#versions-and-releases)
- [Development](#development)
- [History](#history)
- [Roadmap](#roadmap)
- [License](#license)

## Install

```bash
go get github.com/franchb/go-pcre2@v0.49.0
```

Requires Go 1.26 or later. The default build needs no C toolchain, and it has no dependencies outside the standard library.

## When to use it

Go's `regexp` is RE2: linear-time and safe, but with no lookaround or backreferences. [dlclark/regexp2](https://github.com/dlclark/regexp2) covers most PCRE-style patterns in pure Go. It does not cover these PCRE2 features, which this package does:

| Feature | Syntax |
|---|---|
| Reset match start | `\K` |
| Whole-pattern recursion | `(?R)` |
| Subroutine calls | `(?1)`, `(?&name)` |
| Branch reset | `(?\|a\|b)` |

Pick the least powerful engine that fits your patterns:

1. **Go's `regexp`**, when RE2 syntax is enough. It is linear-time, so hostile input can't make it backtrack.
2. **regexp2 or go-pcre2**, when you need lookaround or backreferences. They perform about the same on simple patterns (see [Performance](#performance)). regexp2 has a timeout; go-pcre2 has PCRE2's match, depth and heap limits.
3. **go-pcre2**, when a pattern needs PCRE2 itself: one of the features in the table, or exact PCRE2 semantics for a rule set written against PCRE.

Callouts `(?C…)` are not supported; see the [roadmap](#roadmap).

## API

Close to `regexp`, except that match methods return an `error`, because a backtracking match can hit a limit.

```go
re, err := pcre2.Compile(`(?<year>\d{4})-(?<month>\d\d)`, pcre2.Caseless())
var ce *pcre2.CompileError
if errors.As(err, &ce) {
	fmt.Println(ce.Offset, ce.Message) // byte offset in the pattern, PCRE2's message
}

m, err := re.FindStringSubmatch("on 2026-10")     // ["2026-10" "2026" "10"]
i := re.SubexpIndex("month")                      // 2
all, err := re.FindAllStringIndex(subject, -1)    // [][]int byte offsets
```

**Compiling**

| Function | Notes |
|---|---|
| `Compile(expr, opts...) (*Regexp, error)` | |
| `MustCompile(expr, opts...) *Regexp` | Panics on error |

**Matching.** Only `Match` and the index methods have `[]byte` forms:

| `string` methods | `[]byte` methods | Returns |
|---|---|---|
| `MatchString` | `Match` | `(bool, error)` |
| `FindString`, `FindStringIndex` | `FindIndex` | The leftmost match |
| `FindStringSubmatch`, `FindStringSubmatchIndex` | `FindSubmatchIndex` | The leftmost match with its groups |
| `FindAllString`, `FindAllStringIndex`, `FindAllStringSubmatchIndex` | `FindAllSubmatchIndex` | Successive matches; `n < 0` means all |

**Groups:** `NumSubexp()`, `SubexpNames()`, `SubexpIndex(name)`. `String()` returns the source pattern.

**Options**

| Option | Effect |
|---|---|
| `Caseless()`, `DotAll()`, `Extended()`, `Multiline()` | Same as the `(?i)`, `(?s)`, `(?x)` and `(?m)` flags |
| `UCP()` | Unicode-aware `\d \w \s` and POSIX classes |
| `Bytes()` | Match raw bytes instead of UTF-8 |
| `InvalidUTF()` | Allow invalid UTF-8 in subjects; read [the cost](#behaviour) first |
| `MatchLimit(n)`, `DepthLimit(n)`, `HeapLimit(kib)` | Bound backtracking |
| `NoJIT()` | cgo backend only: use the interpreter, for benchmarking |

**Errors**

| Error | When |
|---|---|
| `ErrMatchLimit`, `ErrDepthLimit`, `ErrHeapLimit`, `ErrNoMemory` | A match hit a limit or ran out of memory |
| `*CompileError` | PCRE2 rejected the pattern; has `Offset`, `Code`, `Message` |
| `*MatchError` | Any other PCRE2 match failure, such as invalid UTF-8 in the subject |
| `*TrapError` | The Wasm module trapped. From a match method, the instance is discarded and the `Regexp` stays usable. `Compile` can also return it, and then there is no `Regexp`. |

**Package values**

| Value | Meaning |
|---|---|
| `Version` | The PCRE2 release in use, for example `"10.49"` |
| `Backend` | `"wasm2go"`, or `"cgo"` with `-tags pcre2_cgo` |
| `JIT` | cgo backend only: whether the linked PCRE2 has the JIT |

## Behaviour

- **Offsets** are byte offsets, as in `regexp`.
- **UTF-8.** Patterns and subjects are UTF-8 by default. Subjects are validated with Go's `utf8.Valid`, which is faster than PCRE2's own check. An invalid subject returns a `*MatchError` carrying PCRE2's error code and message.
- **`InvalidUTF()`** makes invalid sequences fail to match instead. It has a cost: PCRE2 then re-validates the rest of the subject on every internal match call, so `FindAll` becomes quadratic on long inputs.
- **`Bytes()`** matches raw bytes.
- **Character classes.** `\d`, `\w` and `\s` are ASCII unless `UCP()` is set, as in `regexp`.
- **Empty matches in `FindAll*`** follow Perl: after an empty match, the next attempt at the same position must be non-empty, and if that fails the search moves on one character. Go's `regexp` never reports an empty match right after a previous match, so `a*` on `"baaab"` gives four matches here and three there.

## Hostile input

PCRE2 is a backtracking engine, so a bad pattern on the wrong input can take exponential time, whatever the runtime. On untrusted input, set limits and handle the errors:

```go
re := pcre2.MustCompile(pattern, pcre2.MatchLimit(100_000), pcre2.HeapLimit(16<<10))
ok, err := re.MatchString(subject)
if errors.Is(err, pcre2.ErrMatchLimit) { /* treat as a failed rule */ }
```

PCRE2's defaults are a match limit of 10,000,000 and a heap limit of 20,000,000 KiB; the heap limit is more than a 32-bit Wasm memory can hold. In the cgo backend, the JIT ignores `DepthLimit` and `HeapLimit` but still applies `MatchLimit`.

## Concurrency and memory

- **Concurrency.** A `Regexp` is safe for concurrent use. Each goroutine matching at the same time gets its own Wasm instance, with its own copy of the compiled pattern, taken from a `sync.Pool`.
- **No `Close`.** Instances are ordinary Go memory, so the garbage collector frees them.
- **Memory** per `Regexp` grows with the number of goroutines matching it at the same time.

## Performance

[docs/BENCHMARKS.md](docs/BENCHMARKS.md) has the method and full tables. Every engine runs back to back on one CI runner, against native PCRE2 built from the same commit.

`main` after [#7](https://github.com/franchb/go-pcre2/pull/7), 64 KiB corpus, medians of 5, AMD EPYC 7763, MB/s:

| pattern | Go `regexp` | regexp2 | **go-pcre2** | PCRE2 interpreter (cgo) | PCRE2 JIT (cgo) |
|---|---|---|---|---|---|
| `\$\d+\.\d\d` | 152 | 122 | **214** | 392 | 606 |
| `\b(\w+) \1\b` | n/a | 7.2 | **13.5** | 29.9 | 151 |
| `(?<=\$)\d+\.\d\d` | n/a | 55.9 | **52.8** | 122 | 387 |
| `[\w.]+@[\w.]+\.com` | 20.0 | 87.6 | **18.0** | 41.8 | 326 |
| `needle`, no match | 63,252 | 65,715 | **3,557** | 29,926 | 15,730 |
| small `MatchString`, ns | 121 | 182 | **275** | 159 | 109 |

What the table shows:

- **Against regexp2:** go-pcre2 is faster on simple patterns and backreferences, ties on lookbehind, and loses on patterns like the email one.
- **Cost of translation:** against the same PCRE2 interpreter compiled natively, wasm2go costs 1.8–2.3× on the four matching patterns.
- **The no-match literal search is the exception.** There the native interpreter is 8.4× faster than wasm2go, and even beats the JIT.
- **The JIT** adds a further 1.5–7.8× on the matching patterns.
- **Small subjects:** a `MatchString` on a 10-byte subject makes no allocations. Most of its time is `pcre2_match` itself, not the call into the module.

## Security model

- **No imports.** The Wasm module has none; the build fails if PCRE2 ever needs one. Matching has no way to reach files, the network, clocks or the process, whatever the pattern or subject.
- **Bounds-checked memory.** wasm2go keeps linear memory as one Go slice and bounds-checks every access, including in `-unsafe` mode. An out-of-range access panics. The wrapper turns that panic into a `*TrapError` and discards the instance. See wasm2go's [SECURITY.md](https://github.com/ncruces/wasm2go/blob/main/SECURITY.md).
- **Pinned sources, checked output.** The source inputs are pinned:
  - PCRE2's tag is checked against a commit hash;
  - wasm2go is a fixed module version, verified by the Go checksum database.

  The build environment is not fully pinned. The `wasi-sdk-25` and `golang:1.26-trixie` images are referenced by tag rather than digest, and the apt tools in the build stage (`git`, `wabt`) are unpinned. So the build is not reproducible by construction. CI rebuilds on every run and fails if the generated code differs from what is committed, which catches drift; so far every run has produced byte-identical output. `internal/wasm/versions.go` records the `.wasm` SHA-256.
- **No dependencies** outside the standard library. The benchmark module, which needs regexp2, is a separate Go module.

## How it is built

`make generate` runs [`buildtools/wasm/Containerfile`](buildtools/wasm/Containerfile) with podman (or `CONTAINER=docker`):

1. Clone PCRE2 at the tag in [`versions.env`](buildtools/wasm/versions.env), and check that the tag still points at the pinned commit.
2. Build the 8-bit library with wasi-sdk 25, JIT off. PCRE2's JIT has no WebAssembly backend.
3. Link a reactor module that exports only its memory and the functions in [`exports.txt`](buildtools/wasm/exports.txt).
4. Fail the build if the module imports anything.
5. Translate the module to Go with the pinned wasm2go version and its `-unsafe` option, and record the inputs and the `.wasm` SHA-256 in `internal/wasm/versions.go`.

With `-unsafe`, the generated loads and stores do one explicit bounds check on the last byte of each access, then an unaligned `unsafe.Pointer` access. That is about 2.5× faster than the default byte-by-byte code. Big-endian and strict-alignment targets fall back to the default path.

CI regenerates the code on every run. On a PR from this repository it commits the result; on `main` it fails if the result differs from what is committed.

## cgo backend

Building with `-tags pcre2_cgo` swaps the wasm2go backend for cgo bindings to native PCRE2 with the JIT. The API is identical. It exists as the baseline for benchmarks, and for programs that can use cgo but want the same API. [`buildtools/native/build.sh`](buildtools/native/build.sh) builds the same pinned PCRE2 commit natively, with the JIT:

```bash
PREFIX=$HOME/.local/pcre2 buildtools/native/build.sh
```

```bash
PKG_CONFIG_PATH=$HOME/.local/pcre2/lib/pkgconfig go test -tags pcre2_cgo ./...
```

## Versions and releases

Module versions follow PCRE2: `v0.49.x` is PCRE2 10.49.

- **[`pcre2-release.yml`](.github/workflows/pcre2-release.yml)** checks PCRE2's tags daily. When a newer release appears, it opens a PR that bumps the pin and carries the regenerated code, then starts CI on it. This needs **Settings → Actions → General → "Allow GitHub Actions to create and approve pull requests"** enabled.
- **[`release.yml`](.github/workflows/release.yml)** tags `v0.MINOR.0` and publishes a GitHub release when a pin change lands on `main`.
- **Wrapper-only fixes** are released by hand as `v0.MINOR.PATCH`.

## Development

```bash
make generate
```

Rebuilds `internal/wasm`, using podman by default. On SELinux hosts the bind mount gets `:Z`; set `VOLUME_OPTS=` elsewhere.

```bash
make test
```

Runs `go vet` and `go test -race`.

```bash
make bench
```

Runs the benchmark module for the wasm2go backend.

The generated file, `internal/wasm/pcre2.wasm.go` (about 55,000 lines), is committed so that `go get` works without a container. Don't edit it by hand.

## History

All of this was built and measured on 2026-10-28. The first working build ran FindAll at about 2% of regexp2's speed on the simple pattern. Three changes took it to 1.6×:

1. **The first bug was mine.** I had made `PCRE2_MATCH_INVALID_UTF` the default. PCRE2's source says that option "forces a check, overriding the setting of PCRE2_NO_UTF_CHECK", so every call re-validated the subject and `FindAll` went quadratic. Validating once fixed it: [#1](https://github.com/franchb/go-pcre2/pull/1).
2. **Validating UTF-8 in Go**, instead of in PCRE2: [#4](https://github.com/franchb/go-pcre2/pull/4).
3. **wasm2go's `-unsafe` option:** about 2.5× everywhere ([#2](https://github.com/franchb/go-pcre2/pull/2)).

Later, [#7](https://github.com/franchb/go-pcre2/pull/7) stopped allocating offsets a method doesn't return, which took the price pattern to 1.75× regexp2.

The [blog post update](https://franchb.com/posts/pcre-regex-in-go-2026/#update-so-i-built-it) tells the whole story. [docs/BENCHMARKS.md](docs/BENCHMARKS.md) has the numbers at each step.

## Roadmap

- **Callouts.** They need a host import, so they mean a deliberate decision about the no-imports rule.

## License

BSD-3-Clause; see [LICENSE](LICENSE). The generated code includes PCRE2 (BSD-3-Clause WITH PCRE2-exception) and wasi-libc; see [NOTICE](NOTICE).
