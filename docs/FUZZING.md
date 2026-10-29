# Fuzzing

Status: the four targets, the PCRE2 seeds, the per-PR CI steps and the nightly workflow are built; see [What the first runs found](#what-the-first-runs-found). The section names below follow the original plan.

```bash
make fuzz FUZZTIME=5m
```

```bash
PKG_CONFIG_PATH=... go test -tags pcre2_cgo -run '^$' -fuzz '^FuzzDifferential$' -fuzztime 5m .
```

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
- **No refactor was needed.** The plan was to move the cgo code into `internal/native` so both backends could share one test binary. Instead, [`fuzz_diff_test.go`](../fuzz_diff_test.go) is built with `pcre2_cgo`, so the package uses native PCRE2, and the test drives the generated `internal/wasm` module directly with a 60-line driver.
- **`HeapLimit` is left out.** PCRE2's heap frames hold pointers and sizes, which are 4 bytes in Wasm and 8 natively. So the two use different amounts of heap, and a heap limit can trip on one and not the other. `MatchLimit` and `DepthLimit` count steps, so the two agree on them.
- **It skips when the versions differ.** If native PCRE2 isn't the pinned release, the target skips with a message. CI builds native PCRE2 from the pinned commit, so there it always runs.

## Seed corpus

- **The existing tests:** every pattern and subject in `pcre2_test.go`, `callout_test.go` and the benchmark cases.
- **PCRE2's own test data.** The 30 `testdata/testinput*` files in 10.49 have about 9,300 pattern lines, with their subjects, in pcre2test's format. [`buildtools/fuzzseeds`](../buildtools/fuzzseeds/main.go) (`make fuzzseeds PCRE2=...`) extracts the single-line `/pattern/modifiers` patterns, with up to three subjects each, into [`testdata/pcre2-seeds.txt`](../testdata/pcre2-seeds.txt). It applies pcre2test's subject escapes and maps the `i`, `s`, `x`, `m` and `ucp` modifiers to options. From 10.49 that gives 5,522 patterns and 6,681 subjects, 210 KB in total. The targets load the file with `f.Add` rather than one file per input under `testdata/fuzz/`. PCRE2's test data is BSD-licensed, like the code this repository already redistributes.

## CI

Go's fuzzer runs one target per `go test -fuzz` invocation, so each job loops over the targets.

| when | what | budget |
|---|---|---|
| every PR, `build` job | each target with `-fuzz=^FuzzX$ -fuzztime=30s` | about 2 minutes |
| every PR, `cgo` job | `FuzzDifferential`, `-fuzztime=60s` | 1 minute |
| nightly, `fuzz.yml` | each target for 20 minutes, one matrix job per target | 20 minutes of wall time, 80 runner-minutes |

- **The generated corpus** lives in `$GOCACHE/fuzz`. The nightly job saves and restores it with `actions/cache`, keyed by target, so each night starts from the previous night's coverage.
- **A failure** writes the input to `testdata/fuzz/<Target>/`. The job uploads it as an artifact and fails. The fix PR commits that file, and from then on plain `go test` runs it as a regression test on every CI run, with no fuzzing involved.
- **Crashes in PCRE2 itself,** confirmed against native PCRE2, go upstream to PCRE2 rather than being worked around here.

## What the first runs found

Measured on a 4-vCPU Xeon @ 2.80GHz, 30 s per target, starting from the seeds:

| target | execs/s | new interesting inputs |
|---|---|---|
| FuzzCompile | ~2,600 | 123 |
| FuzzMatch | ~4,700 | 215 |
| FuzzCallout | ~7,000 | 284 |
| FuzzDifferential (60 s) | ~6,500 | 128 |

- **Instrumentation cost.** Instrumenting the 55,000-line generated file is affordable. The fuzzer keeps finding new coverage inside the translated PCRE2.
- **No bugs found.** No failures in about 750,000 executions, and no difference between wasm2go and native PCRE2.
- **Seeds as regression tests.** The seeds took 16 s to run as plain tests over the three wasm2go targets, so `go test -race` runs only the hand-written seeds. Plain `go test` and every fuzz run use all of them.

Two invariants had to be corrected against PCRE2's own test data:

- **`\C` splits characters.** `\C` matches a single code unit even in UTF mode (pcre2pattern(3)), so offsets need not fall on character boundaries when the pattern contains it. PCRE2's tests use this on purpose.
- **An older native PCRE2 can return a match that ends before it starts.** Run against the system's PCRE2 10.42, the cgo backend returns `start > end` for `\K` reached through a subroutine call inside a lookahead, such as `(?(DEFINE)(?<sneaky>b\K))a(?=(?&sneaky))` on `ab`. 10.49 rejects these at match time with error -75, "disallowed use of \K in lookaround". 10.42's `pcre2.h` has no such error code; I haven't established which release in between added it. The pinned build, and so the wasm2go backend, is not affected. With the cgo backend and an older system PCRE2, `FindString` on such a pattern would panic while slicing the subject.

## Order of work, as planned

All five steps are done.

1. `FuzzCompile` and `FuzzMatch` with the existing tests as seeds, plus the 30-second PR step. Measure executions per second on the instrumented build.
2. The script that turns PCRE2's test data into seeds.
3. `FuzzCallout`.
4. ~~The `internal/native` refactor~~ (not needed) and `FuzzDifferential` in the `cgo` job.
5. The nightly `fuzz.yml` with the cached corpus.
