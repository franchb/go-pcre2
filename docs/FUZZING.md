# Fuzzing roadmap

Status: planned, nothing below is built yet.

## What there is to find

PCRE2's own C code is already fuzzed upstream. It ships a libFuzzer harness, [`src/pcre2_fuzzsupport.c`](https://github.com/PCRE2Project/pcre2/blob/pcre2-10.49/src/pcre2_fuzzsupport.c), runs on [OSS-Fuzz](https://google.github.io/oss-fuzz/), and its ChangeLog cites the ClusterFuzz issues it fixed. Fuzzing PCRE2's logic again here would mostly repeat that work.

What OSS-Fuzz does not cover is the layers this repository adds:

1. **The translation.** wasm2go turns about 400 KB of Wasm into 55,000 lines of Go. A wrong translation of one instruction would give wrong results without crashing.
2. **The wrapper.** The wrapper does several things that could go wrong without crashing:
   - it copies subjects into linear memory and reads offsets back;
   - it validates UTF-8 in Go and then tells PCRE2 to skip its own check;
   - it implements Perl's empty-match rule in `FindAll`;
   - it pools instances and drops any instance whose match trapped.
3. **The callout path.** Here PCRE2 calls into Go, Go reads PCRE2's callout block at fixed offsets, and a panic or error has to get back out through PCRE2.

The fuzz targets should aim at those three.

## Tooling

Go's native fuzzing (`go test -fuzz`), with no new dependencies. With `-fuzz`, `go test` instruments every package the test imports, except a short list of runtime and testing packages (`skipInstrumentation` in `cmd/go/internal/test/test.go`). So `internal/wasm` is instrumented, and the fuzzer is coverage-guided through the translated PCRE2 itself. Whether instrumenting a 55,000-line file makes each execution too slow is the first thing to measure.

## Targets

Every target compiles with `MatchLimit` and `HeapLimit` set, so each input finishes in bounded time. Go's fuzzer has no per-input timeout to rely on.

### FuzzCompile(pattern string, flags uint32)

- `Compile` returns a `*Regexp` or a `*CompileError`. It never panics. A `*TrapError` is always a bug.
- For a `*CompileError`, `Offset` is within the pattern.
- `NumSubexp`, `SubexpNames` and `SubexpIndex` agree with each other.

### FuzzMatch(pattern, subject string, flags uint32)

Invariants on every result:

- **Offsets.** `0 <= start <= end <= len(subject)`. A group that did not take part is `-1, -1`. Under UTF mode, every offset falls on a rune boundary.
- **Consistency between methods.**
  - `MatchString` is true exactly when `FindStringIndex` is non-nil.
  - `FindStringIndex` is the first pair of `FindStringSubmatchIndex`.
  - The first match of `FindAll*` equals `Find*`.
- **`FindAll` order.** Matches do not overlap and do not move backwards. An empty match at the previous match's end is allowed: that is the Perl rule.
- **`[]byte` and `string` give the same results.**
- **Errors.** Invalid UTF-8 in UTF mode returns a `*MatchError`, not a panic. A limit error comes back as `ErrMatchLimit` or `ErrHeapLimit`.
- **Instance reuse.** After any error, the next match on the same `Regexp` agrees with a freshly compiled one. This catches state that leaks through the pool.

### FuzzCallout(pattern, subject string, script []byte)

`script` decides what each callout returns: continue, `Fail`, an error, or a panic.

- **An error** comes back unchanged from the match method.
- **A panic** reaches the caller with its original value, and the `Regexp` still matches correctly afterwards.
- **Inside the callout,** `Callout.Group(i)` offsets are within the subject and `Position <= len(subject)`.
- **Continue everywhere.** A script that always continues gives the same match as compiling the same pattern without `OnCallout`.

### FuzzDifferential(pattern, subject string, flags uint32)

This is the strongest oracle. The wasm2go module and native PCRE2 run the same C code from the same commit, so with the JIT off they must agree exactly: the same return code and the same ovector. Any difference is a bug in wasm2go's translation or in one of the two wrappers.

- **Compare with the JIT off only.** With the JIT on, results can differ legitimately: the JIT ignores `DepthLimit` and `HeapLimit`, and it reports no callout flags.
- **Refactor needed first.** The two backends are selected by a build tag, so today they can't be linked into one test binary. Moving the cgo code into `internal/native`, behind the existing `pcre2_cgo` tag, lets a `pcre2_cgo` fuzz test import both `internal/wasm` and `internal/native` and call `pcre2_match` on each.

## Seed corpus

- **The existing tests:** every pattern and subject in `pcre2_test.go`, `callout_test.go` and the benchmark cases.
- **PCRE2's own test data.** The 30 `testdata/testinput*` files in 10.49 have about 9,300 pattern lines, with their subjects, in pcre2test's format. A small script in `buildtools/` extracts the plain `/pattern/` lines and their subject lines into `testdata/fuzz/<Target>/`. They are BSD-licensed, like the code we already redistribute.

## CI

Go's fuzzer runs one target per `go test -fuzz` invocation, so each job loops over the targets.

| when | what | budget |
|---|---|---|
| every PR, `build` job | each target with `-fuzz=^FuzzX$ -fuzztime=30s` | about 2 minutes |
| every PR, `cgo` job | `FuzzDifferential`, `-fuzztime=60s` | 1 minute |
| nightly, new `fuzz.yml` | each target for 20 minutes, both jobs | about 2 hours |

- **The generated corpus** lives in `$GOCACHE/fuzz`. The nightly job saves and restores it with `actions/cache`, keyed by target, so each night starts from the previous night's coverage.
- **A failure** writes the input to `testdata/fuzz/<Target>/`. The job uploads it as an artifact and fails. The fix PR commits that file, and from then on plain `go test` runs it as a regression test on every CI run, with no fuzzing involved.
- **Crashes in PCRE2 itself,** confirmed against native PCRE2, go upstream to PCRE2 rather than being worked around here.

## Order of work

1. `FuzzCompile` and `FuzzMatch` with the existing tests as seeds, plus the 30-second PR step. Measure executions per second on the instrumented build.
2. The script that turns PCRE2's test data into seeds.
3. `FuzzCallout`.
4. The `internal/native` refactor and `FuzzDifferential` in the `cgo` job.
5. The nightly `fuzz.yml` with the cached corpus.
