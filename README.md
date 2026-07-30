# uda - the universal dependency analyzer

A CLI tool for identifying refactoring candidates in code by measuring structural coupling (imports) and evolutionary coupling (git history) across packages.

Uses tree-sitter for static analysis. Supports Go, TypeScript, Rust, Python, and Swift.

## Installation

```sh 
go install github.com/flamingoosesoftwareinc/uda@latest
```


This will install to your GOBIN directory so please ensure it is on the path.

## Interpreting the Metrics

**Inward Coupling** — how many external packages depend on this package (dependents). High Ic means changes here are risky; they ripple outward.

**Outward Coupling** — how many external packages this package depends on (Dependencies). High Oc means this package is sensitive to upstream changes.

**Instability** — Oc / (Ic + Oc). Ranges from 0 (stable, everyone depends on you) to 1 (unstable, you depend on everyone).

These metrics are inspired by Robert Martin's afferent/efferent coupling defined in [**Agile software development: principles, patterns and practices**](https://www.amazon.ca/Software-Development-Principles-Patterns-Practices/dp/0135974445)

You can also read about them on [wikipedia](https://en.wikipedia.org/wiki/Software_package_metrics)

### What to look for

| Pattern | Signal | Action |
|---------|--------|--------|
| high Ic, low instability, concrete types | Zone of pain — hard to change, everyone depends on it | Consider extracting an interface |
| high Ic, low instability, mostly interfaces | Healthy abstraction | Keep it stable, protect with tests |
| high Oc, high instability | Implementation package | Expected for leaf nodes. Consider interfaces at real boundaries (repos, clients, infra) |
| Low Ic, low Oc | Isolated | Check if it's dead code |
| high Ic, high Oc | Hub - many dependents and dependencies | Potential god package, consider splitting |

### Rules of thumb

- Packages with high inward coupling deserve more scrutiny during code review
- Before adding a dependency to a low-instability package, ask if it belongs there
- High instability isn't bad — implementations *should* be easy to change
- The red flag is a concrete package that's stable and heavily depended upon
- Low Oc via dependency inversion is only valuable if the interfaces are genuinely swappable. Defining interfaces solely to reduce coupling metrics without enabling substitution is cosmetic and obfuscates real coupling between packages

## Why?

The last year I reviewed more code than ever. It has become difficult to keep up with the sheer volume of output with agentic coding, and this train doesn't look like it is going to slow down.

This tool helps me to at least identify areas of concern with respect to Change Amplification. Change Amplification is when making a change in one part of the application requires making changes to another. It can be caused by tight coupling, lack of abstraction, duplicated logic, etc.

In my experience, Change Amplification risk accumulates over time and is one of the largest wastes of time. At the lowest level I'm looking for
1. Is this package going to be easy to change?
2. Is this package volatile (depending on a lot of other packages)?

This tool is designed to help identify these areas of concern.

## Evolutionary Coupling

`uda coupling` detects packages that change together over time, revealing dependencies that static import analysis misses. Uses kernel-based temporal correlation on git history.

```sh
uda coupling --since 90d --min-correlation 0.3
uda coupling --since 180d --target-precision 0.05 --format json /path/to/repo
uda coupling --since 90d --sigma 14d --min-correlation 0.3   # manual override
uda coupling --filter 'pkg/auth' --since 90d
```

**Sigma** controls the kernel bandwidth — how far in time a commit's influence extends. Small sigma (7d) = strict, only near-simultaneous changes register. Large sigma (30d) = loose, catches slow-moving coupling.

By default sigma is **auto-derived** from a precision target — the user expresses how stable they want correlation values to be, and the picker chooses the smallest sigma that delivers that stability across the window. Pass `--target-precision=<float>` (default 0.05 ≈ two decimal places) to control the knob, or `--sigma=<duration>` to override entirely.

JSON output includes a `sigma_selection` block describing the chosen sigma, the required effective sample size, and what was actually achieved across the analysis window:

```json
{
  "sigma_selection": {
    "mode": "auto",
    "sigma_seconds": 1209600,
    "sigma_human": "14d",
    "target_stddev": 0.05,
    "required_ess": 400,
    "achieved_ess_median": 412,
    "achieved_ess_p25": 287,
    "low_confidence": false
  },
  "pairs": [ ... ]
}
```

The picker is **density-aware**: precision is evaluated at commit-time quantiles (not wall-clock), and when the history has work-session structure (bursts of commits separated by large gaps — typical of agent-driven development), sigma is capped at session scale so distinct sessions stay distinguishable. The derived ceiling is reported as `density_cap_seconds`/`density_cap_human`.

`low_confidence: true` means the precision target was unreachable. `reason: "density_fallback"` — sigma was held at session scale and precision sacrificed (a wider sigma would only produce a precise estimate of a meaningless blend); check the achieved ESS to judge how noisy the values are. `reason: "insufficient_commits"` — the window is too sparse and has no session structure; the picker falls back to `window/3`. Either way, widen `--since` or loosen `--target-precision` to trade off differently.

**Correlation** ranges from 0 (no temporal relationship) to 1 (always change together).

### Mutual Information (default-on)

Binned mutual information runs alongside kernel-weighted Pearson by default. Bin width = the same auto-derived (or manual) sigma. Sort by `divergence` (`nmi - pearson_binned`) to surface **sparse-but-reliable** couplings that Pearson misses (e.g. `payments` always changes with `compliance-audit` but only 4 times in 90 days).

```sh
uda coupling --since 365d --sort divergence --format json
uda coupling --mi=false                                 # disable if you only want correlation
```

Per-pair fields: `pearson_binned`, `nmi`, `divergence`. Top-level `mi_selection` block reports bin width, count, and `low_confidence: true` when bins < 8 (widen `--since` or shorten sigma to fix). NMI uses Miller–Madow bias correction; suppressed pair-wise (field absent) when the contingency is too sparse. `--mi-min-support=N` (default 3) controls the co-occurrence floor.

### What to look for

| Pattern | Signal |
|---------|--------|
| High correlation, no import edge | Hidden dependency — coupling exists but isn't in the code structure |
| High correlation + high structural coupling | Confirmed tight coupling — both static and temporal |
| Asymmetric (A always changes with B, but B changes alone too) | Driver/dependent relationship |
| Low correlation + high NMI (positive divergence) | Sparse-but-reliable coupling — Pearson misses it; investigate |
| High correlation + high NMI (≈ zero divergence) | Both metrics agree — well-established coupling |

## Lint — coupling as policy

`uda lint` enforces the structural assumptions your architecture makes, from a
lockfile-style policy in the repo-local `.uda.yaml`:

```sh
uda lint init      # snapshot the current graph into allowed: — adoption starts green
uda lint           # exit 1 on any unlisted, forbidden, pending, or stable-origin edge
uda lint accept    # stage new edges as pending: (dated, commit-attributed) — lint stays red
uda lint approve   # move pending -> allowed; forbidden edges are rejected even here
```

Every internal package-to-package edge must be listed. A new dependency fails
the gate until a human (or reviewing agent) moves it from `pending:` to
`allowed:` — the yaml diff in the PR *is* the coupling review. Standard library
and third-party targets are out of scope; test packages and `testdata` trees
are excluded by default (`lint.exclude` extends this with globs).

```yaml
lint:
  exclude: ["poc/**"]
  go:
    roles:
      stable: [internal/domain]   # new outbound edges here are never auto-acceptable
    forbid:
      - from: internal/domain
        to: internal/adapter/**   # can never be allowed, not even via approve
    allowed:
      - cmd -> internal/analyzer
    pending: []
  python:
    boundary: module              # per-language granularity override
    allowed: []
    pending: []
```

Each language block accepts a `boundary` override matching the analyzer's
strategies (Python: `module` / `package` / `subpackage`; Rust: `package` /
`module`; TypeScript: `package` / `barrel` / `directory`). It defaults to
`package`. Set it when the default collapses too much — e.g. Python `package`
folds every module under `mypkg/` into one node, hiding intra-package edges;
`module` keeps them distinct. Re-run `uda lint init` after changing it to
regenerate the lockfile at the new granularity.

Config resolves repo-first: the repo-local `.uda.yaml` merges over the
user-scope `$XDG_CONFIG_HOME/uda/config.yaml` (repo wins). `uda config promote`
copies the general params — never the lint block — up to user scope.

## Review advisories — co-change as review signal

`uda review` additionally reports **co-change advisories**: packages that
historically change together with what the diff touched but were left alone
this time. Informational only — advisories never affect the exit code, and
history-dependent signals deliberately stay out of `uda lint`.

```
Co-change advisories (informational):
  internal/api changed without internal/api/docs (correlation 0.83)
```

The co-change model is built from the window before the review head (not the
wall clock, so range reviews reproduce), with the density-aware sigma.
Low-support pairs are suppressed by default. Tune via the shared `advisory:`
config block:

```yaml
advisory:
  coupling:
    since: 90d
    min-correlation: 0.6
    include-low-support: false
    # sigma: 4h              # optional manual override
```

Disable per run with `--advisory=false`.

## Will this work with AI agents?

It's a CLI tool, agnostic of any AI agent or its interface. This tool can be used from the terminal (in non-interactive environments will return JSON)

MCP is supported over stdin `uda mcp start`

## Limitations

Because uda uses tree-sitter for static syntax analysis (not semantic analysis), there are language-specific edges it cannot cover.

### Rust

The Rust analyzer tracks dependencies declared via `use` statements and their usages (type annotations, trait bounds, function calls, struct initialization, etc.).

**Not tracked:**
- **Glob imports** (`use foo::*`) — intentionally unsupported as they obscure actual dependencies
- **Fully qualified inline paths** (e.g., `std::collections::HashMap::new()` without a `use` statement) — capturing all qualified paths would also pick up prelude types like `String::from()` and `Vec::new()`, which are not meaningful dependencies
- **`mod` statements** — these declare module structure, not usage; actual coupling is tracked via `use` statements
- **`pub use` re-exports** — usages are attributed to the re-exporting module, not traced back to the original source

### Go

The Go analyzer tracks dependencies declared via `import` statements and their usages (qualified type references, selector expressions).

**Not tracked:**
- **Dot imports** (`import . "pkg"`) — intentionally unsupported as they obscure actual dependencies and make usage attribution ambiguous
- **Blank imports** (`import _ "pkg"`) — these are side-effect imports with no direct usage to track

### Python

The Python analyzer tracks dependencies declared via `import` and `from ... import` statements, including relative imports within packages. Both traditional packages (with `__init__.py`) and namespace packages (PEP 420) are supported.

**Not tracked:**
- **Star imports** (`from module import *`) — intentionally unsupported as they obscure actual dependencies
- **Dynamic imports** (`importlib.import_module()`, `__import__()`) — runtime imports cannot be statically analyzed
- **Bare identifier usages** — names used as plain arguments without attribute access or calls (e.g., `hasattr(sys, "ps1")`, `isinstance(x, Config)`) are not tracked at the usage level, since they are syntactically indistinguishable from local variables; the dependency edge from the import statement is still recorded

### TypeScript

The TypeScript analyzer tracks dependencies declared via `import` statements (named, default, namespace, and type-only imports).

**Not tracked:**
- **Dynamic imports** (`import("./module")`) — runtime imports cannot be statically analyzed
- **`require()` calls** — CommonJS syntax; use ES module imports instead
- **Glob/wildcard re-exports** (`export * from "./module"`) — intentionally unsupported as they obscure actual dependencies

### Swift

The Swift analyzer tracks dependencies declared via `import` statements and their usages (type references, protocol conformances, function calls, etc.).

**Not tracked:**
- **@_exported imports** — re-exports hide actual dependencies; usages are attributed to the re-exporting module, not the original source
- **Implicit Foundation imports** — Swift standard library types like `String`, `Array`, `Int` are available without explicit import; these fundamental types are not tracked as external dependencies
- **SPM internal modules** — internal vs public visibility within Swift Package Manager targets is not analyzed; all types in imported modules are treated equally
- **Objective-C bridging** — types imported via bridging headers or `@objc` interop are not tracked; only Swift module imports are analyzed

## Dependencies

- [go-enry](https://github.com/go-enry/go-enry)
- [go-tree-sitter](https://github.com/tree-sitter/go-tree-sitter)
- [cobra](https://github.com/spf13/cobra)
- [ophis](https://github.com/njayp/ophis)
