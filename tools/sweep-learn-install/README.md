# sweep-learn-install

Retrofits the self-learning loop (`internal/learn` package + supporting
wiring) into every per-CLI entry under `library/<cat>/<api>/`. Sister
tool to `tools/sweep-canonical/`; same GOPATH-mode + idempotency
contract, but does Go-source surgery instead of markdown patches.

## When to run

Run this after `cli-printing-press`'s learn-loop templates change in a
way that the published library should adopt. Fresh prints from the
generator already produce the canonical shape; this tool brings
existing entries up to the same shape without a full regeneration.

The contract for when an upstream template change requires a parallel
sweep run lives in `cli-printing-press`'s AGENTS.md under
"Cross-repo dependency: published-library sweep tool" (the learn-loop
section). The tracking-issue flow described there applies.

## Invocation

From the repo root:

```bash
SWEEP_LIBRARY_ROOT=library GO111MODULE=off go run ./tools/sweep-learn-install
```

Flags:

- `-readme-only` / `--readme-only` / `SWEEP_README_ONLY=1` — patch
  only `SKILL.md`, skip Go-source surgery.
- `-dry-run` / `--dry-run` / `SWEEP_DRY_RUN=1` — print planned
  changes per CLI, write nothing.
- `-only=<slug>` — restrict to a single library entry for debugging.

Tests:

```bash
cd tools/sweep-learn-install && GO111MODULE=off go test .
```

## What the sweep does, per CLI

Atomic per directory; any error rolls back every file written for that
CLI from an in-memory snapshot before moving on. Non-zero exit if any
CLI errored.

1. Skip if `.printing-press.json` is absent.
2. Skip if `.no-learn-sweep` opt-out marker is present.
3. Refuse if `internal/cli/root.go` uses the legacy `var rootCmd`
   shape (agent-capture / instacart style). Reported as
   `manual review required`.
4. Skip if `internal/store/store.go` is missing the
   `// CLI Printing Press: learn migrations` anchor — store.go is
   presumed hand-modified.
5. Render the `internal/learn/*.go` package files. **Byte-for-byte
   parity** with `cli-printing-press` is enforced by
   `TestRenderLearnPackage_ByteForByteParity` against the
   `generate-learn-loop-api` golden fixture.
6. AST-inject `internal/cli/root.go` to add the `--no-learn` flag,
   the teach/recall/learnings `AddCommand` calls, and the
   `learnHookSkipList` map.
7. Rewrite the learn-migrations block in `store.go` and bump
   `StoreSchemaVersion` to 3.
8. Patch `SKILL.md` to add the Automatic Learning section
   (idempotent strip-then-re-emit between
   `<!-- pp-learn-section-start -->` /
   `<!-- pp-learn-section-end -->` anchors).
9. Add `modernc.org/sqlite` to `go.mod` if missing, then run
   `go mod tidy` in the CLI directory.
10. Update `printing_press_version` in `.printing-press.json`.

## What this tool does NOT do

- No `.printing-press-patches.json` entry. The learn loop is a
  generator-owned package, not a per-CLI patch.
- No per-CLI Learn config (ticker patterns, stopwords, entity-lookup
  seeds). The sweep emits the stub `newLearnConfig` / `initLearn`
  shape; operators add per-CLI Learn data by hand-editing
  `internal/cli/learn_init.go` after the sweep.
- No edits under `tools/sweep-canonical/`. The two tools are
  siblings, not chained.

## Supported root.go shapes

`internal/cli/root.go` ships in three shapes across the library; the
sweep auto-supports two and refuses the third.

1. **Canonical struct-based shape.** A package-level
   `type rootFlags struct{}` plus either
   `Execute()` declaring `var flags rootFlags` locally, or
   `func newRootCmd(flags *rootFlags) *cobra.Command`. The sweep
   auto-detects which form is in scope and emits the correct
   `&flags` (value) or `flags` (pointer) argument for each
   `new<X>Cmd` constructor call.
2. **`func Root() *cobra.Command` factory** (instacart's shape).
   External factory with no `rootFlags` struct in scope. The sweep
   patches this shape by emitting a tiny
   `internal/cli/learn_root_shim.go` carrying the canonical
   `rootFlags` struct (three booleans: `noLearn`, `dryRun`,
   `asJSON`) and splicing the `--no-learn` flag binding plus the
   five `new<X>Cmd` `AddCommand` calls just before the factory's
   final `return root` statement. Canonical-shape CLIs already
   declare `rootFlags` in their own `root.go` and do NOT receive
   the shim; emitting it there would clash.
3. **Legacy `var rootCmd`.** Agent-capture-style package-global
   command with no `rootFlags` struct and no factory function.
   The sweep refuses with a "manual review required" diagnostic
   and continues.

## Factory-shape root.go support

When the sweep detects the factory shape (`func Root() *cobra.Command`
or `func RootCmd() *cobra.Command` with no surrounding `rootFlags`
struct), it takes a slightly different path than the canonical
sweep:

- `internal/cli/learn_root_shim.go` is **emitted** from
  `templates/cli/learn_root_shim.go.tmpl`. It declares the same
  `rootFlags` struct the generator template emits in `root.go` for
  canonical CLIs, scoped down to the three fields the sweep's
  `teach.go` reads (`noLearn`, `dryRun`, `asJSON`). Operators are
  free to extend the struct; the sweep only depends on those three.
- The factory function's body is **patched in-place**: a
  `learnCfg := newLearnConfig()` + `var learnFlags rootFlags` pair
  plus the `--no-learn` `PersistentFlags().BoolVar` binding plus
  the five `AddCommand` calls splice in just before the factory's
  final `return <ident>`. The return identifier is detected via
  AST so `return root`, `return rootCmd`, `return cmd` all work.
- The shared `learnHookSkipList` map + `shouldSkipLearnHook` helper
  append at file end identically to the canonical path.

This is idempotent: the second run finds `newTeachCmd(` already
present in the file and skips the body splice; the shim's identifier
is `rootFlags` so the canonical detection path also recognizes it as
"already has a rootFlags struct" and would attempt the canonical
flow on a re-sweep — but the `newTeachCmd(` probe inside
`injectLearnAddCommands` short-circuits that case too.

## Divergence from the generator's `teach.go.tmpl`

The sweep's `templates/cli/teach.go.tmpl` is **not** a byte-for-byte
copy of the upstream generator's `internal/generator/templates/teach.go.tmpl`.
Three deliberate divergences exist so the emission compiles against
older library CLIs whose `internal/cli/` packages predate the modern
`helpers.go` baseline:

| Generator emits | Sweep emits | Why |
|---|---|---|
| `dryRunOK(flags)` | `learnDryRunOK(flags)` | Older library CLIs don't carry `dryRunOK`. Inlined private equivalent. |
| `parentNoSubcommandRunE(flags)` | `learnParentNoSubcommandRunE(flags)` | Same. Inlined as a closure with the canonical machine-readable error shape. |
| `printJSONFiltered(w, v, flags)` | `learnPrintJSON(w, v, flags)` | Older CLIs lack the `printOutputWithFlags` plumbing the canonical helper rides on. Falls back to a minimal `json.MarshalIndent` shape that does not honor `--select` / `--csv` / `--compact`. |
| `store.OpenWithContext(ctx, dbPath)` | `store.Open(dbPath)` | The context-aware variant is a newer addition; older CLIs ship only `store.Open`. |

The byte-for-byte parity test for `internal/cli/teach.go` is
deliberately gone (replaced by
`TestEmittedTeachGo_NoExternalHelperDeps`); the parity check still
runs against every file under `internal/learn/`, which has no host-
CLI dependencies and stays in sync with the generator.

The sweep also back-fills a `func (s *Store) DB() *sql.DB` accessor on
`internal/store/store.go` when missing (see `ensureStoreDBAccessor`)
since the sweep-emitted `teach.go` calls `s.DB()` to thread the
underlying `*sql.DB` into the `internal/learn` package.

## Embedded templates

`templates/learn{,_entities,_lookups,_patterns}/*.tmpl` are verbatim
copies of the upstream templates from
`cli-printing-press/internal/generator/templates/`. They are embedded
via `go:embed` so the sweep tool ships with the exact source the
generator parses, and the parity test fails if the local copies drift
from upstream.

When the upstream templates change in a way the library should adopt,
copy the updated files here and re-run the parity test before
committing.
