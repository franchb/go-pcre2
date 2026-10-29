# Benchmarks

Numbers come from CI, not from a developer machine. The `cgo` job in [`ci.yml`](../.github/workflows/ci.yml) builds native PCRE2 from the same pinned commit as the Wasm build, then runs [`bench/`](../bench/bench_test.go) for both backends back to back on one runner. Each benchmark runs 5 times, and the tables show medians.

- **Corpus:** 64 KiB of generated log-like lines, the same on every run.
- **Engines compared:** Go's `regexp`, `dlclark/regexp2` v2.11.0, go-pcre2 on wasm2go (the default), and native PCRE2 10.49 through cgo, with the JIT on and off (`NoJIT()`).
- **Comparability:** GitHub's runners vary (EPYC 7763, 9V74 and 9V45 have all appeared), roughly 2× apart. Compare engines within one run, not across runs.

## After #7 (commit 1b011a1, AMD EPYC 7763, 4 vCPU)

[#7](https://github.com/franchb/go-pcre2/pull/7) stopped the wrapper from allocating offsets that a method doesn't return. This changed both backends.

Throughput in MB/s; the "small" row is ns per call.

| pattern | stdlib | regexp2 | go-pcre2 (wasm2go) | PCRE2 interpreter (cgo) | PCRE2 JIT (cgo) |
|---|---|---|---|---|---|
| `[\w.]+@[\w.]+\.com` | 20.0 | 87.6 | 18.0 | 41.8 | 326 |
| `\$\d+\.\d\d` | 152 | 122 | 214 | 392 | 606 |
| `(?<=\$)\d+\.\d\d` | n/a | 55.9 | 52.8 | 122 | 387 |
| `\b(\w+) \1\b` | n/a | 7.17 | 13.5 | 29.9 | 151 |
| `needle`, no match | 63,252 | 65,715 | 3,557 | 29,926 | 15,730 |
| small `MatchString`, ns | 121 | 182 | 275 | 159 | 109 |

What changed against v0.49.0. Both runs were on an EPYC 7763, but on different runners, so small differences are noise:
- **Small `MatchString`:** 323 → 275 ns on wasm2go, now with 0 allocations. On cgo the JIT went from 154 to 109 ns and the interpreter from 209 to 159 ns. The saving is about 50 ns on both backends, which is a larger share of the shorter cgo call.
- **FindAll** allocates 19 times per call instead of about one per match (1,013 on this corpus). Price went from 190 to 214 MB/s on wasm2go and from 457 to 606 MB/s on the JIT.
- **Translation cost** is now 1.8× to 2.3× against the native interpreter on the four matching patterns, and 8.4× on the `needle` search.
- **Where the small call goes.** A profile of the small call on wasm2go shows that most of it is now `pcre2_match` itself. Entering the module is a plain Go method call.

## v0.49.0 (commit d14af3c, AMD EPYC 7763, 4 vCPU)

Throughput in MB/s; the "small" row is ns per call.

| pattern | stdlib | regexp2 | go-pcre2 (wasm2go) | PCRE2 interpreter (cgo) | PCRE2 JIT (cgo) |
|---|---|---|---|---|---|
| `[\w.]+@[\w.]+\.com` | 20.3 | 83.5 | 17.7 | 44.8 | 273 |
| `\$\d+\.\d\d` | 156 | 118 | 190 | 296 | 457 |
| `(?<=\$)\d+\.\d\d` | n/a | 53.7 | 51.3 | 117 | 318 |
| `\b(\w+) \1\b` | n/a | 6.83 | 13.4 | 30.1 | 140 |
| `needle`, no match | 63,140 | 65,698 | 3,556 | 29,641 | 15,708 |
| small `MatchString`, ns | 119 | 176 | 323 | 209 | 154 |

What the table shows:
- **Translation cost.** On the four patterns that match, wasm2go costs 1.5× to 2.5× against the same PCRE2 interpreter compiled natively. The no-match `needle` search is the exception: there the native interpreter is 8.3× faster than wasm2go (29,641 vs 3,556 MB/s).
- **The JIT** is a separate multiplier on the four matching patterns: 1.5× to 6.1×. On the `needle` search it is slower than the interpreter (15,708 vs 29,641 MB/s), so it is not a uniform win.
- **go-pcre2 vs regexp2.** go-pcre2 beats regexp2 on the price and backreference patterns, ties on lookbehind, and loses on the email pattern. On that pattern regexp2 also beats PCRE2's own native interpreter.
- **Literal search.** Both Go engines are far ahead on the `needle` search. I haven't profiled why; a literal-prefix fast path is the likely explanation.

## History

How each change moved go-pcre2 relative to regexp2 in the same run:

| change | price `\$\d+\.\d\d`, go-pcre2 ÷ regexp2 |
|---|---|
| first build (939eed7), `PCRE2_MATCH_INVALID_UTF` on by default | 0.017 |
| validate the subject once per FindAll (23a5ca8) | 0.84 |
| UTF-8 validation in Go (#4, 46074f7) | 1.00 |
| wasm2go `-unsafe` (#2), measured on main at d14af3c | 1.61 |
| offsets read only as needed, chunked FindAll results (#7, 1b011a1) | 1.75 |

The raw benchmark output for each run is in the `bench` artifact of the run.
