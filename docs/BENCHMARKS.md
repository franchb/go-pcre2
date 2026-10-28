# Benchmarks

Numbers come from CI, not from a developer machine. The `cgo` job in [`ci.yml`](../.github/workflows/ci.yml) builds native PCRE2 from the same pinned commit as the Wasm build, then runs [`bench/`](../bench/bench_test.go) for both backends back to back on one runner. Each benchmark runs 5 times, and the tables show medians.

- **Corpus:** 64 KiB of generated log-like lines, the same on every run.
- **Engines compared:** Go's `regexp`, `dlclark/regexp2` v2.11.0, go-pcre2 on wasm2go (the default), and native PCRE2 10.49 through cgo, with the JIT on and off (`NoJIT()`).
- **Comparability:** GitHub's runners vary (EPYC 7763, 9V74 and 9V45 have all appeared), roughly 2× apart. Compare engines within one run, not across runs.

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
- **Translation cost.** Against the same PCRE2 interpreter compiled natively, wasm2go costs 1.5× to 2.5×.
- **The JIT** is a separate multiplier on top: 1.5× to 6.1× on these patterns.
- **go-pcre2 vs regexp2.** go-pcre2 beats regexp2 on the price and backreference patterns, ties on lookbehind, and loses on the email pattern, which tries a match at almost every position.
- **Literal search.** Go's engines answer `needle` with a vectorized `bytes.Index`; PCRE2 cannot.

## History

How each change moved go-pcre2 relative to regexp2 in the same run:

| change | price `\$\d+\.\d\d`, go-pcre2 ÷ regexp2 |
|---|---|
| first build (939eed7), `PCRE2_MATCH_INVALID_UTF` on by default | 0.017 |
| validate the subject once per FindAll (23a5ca8) | 0.84 |
| UTF-8 validation in Go (#4, 46074f7) | 1.00 |
| wasm2go `-unsafe` (#2), measured on main at d14af3c | 1.61 |

The raw benchmark output for each run is in the `bench` artifact of the run.
