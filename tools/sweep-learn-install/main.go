// Command sweep-learn-install idempotently retrofits the self-learning
// loop (internal/learn package + supporting wiring) into every per-CLI
// entry under library/<cat>/<api>/. It mirrors the contract owned
// upstream by cli-printing-press's learn templates: fresh prints emit
// the canonical shape directly; this tool retrofits existing entries.
//
// Per-CLI scope (atomic with snapshot-restore on per-CLI failure):
//
//  1. Read .printing-press.json (skip directory if absent).
//  2. Honor .no-learn-sweep opt-out marker.
//  3. Detect internal/cli/root.go shape — refuses on the legacy
//     `var rootCmd` package-global form (per
//     printing-press-library/AGENTS.md "CLI root.go shape" section).
//  4. Snapshot every file the sweep would touch, including go.mod /
//     go.sum / .printing-press.json.
//  5. Write internal/learn/*.go (byte-for-byte parity with the
//     generator's template emission).
//  6. AST-inject internal/cli/root.go: teach/recall/learnings command
//     registrations + the --no-learn persistent flag + the
//     learnHookSkipList machinery.
//  7. Extend internal/store/store.go migrations slice + bump
//     StoreSchemaVersion. Anchor-based: looks for the canonical
//     `// CLI Printing Press: learn migrations` marker. If the anchor
//     is missing the CLI is SKIPPED — store.go is presumed
//     hand-modified and outside the sweep contract.
//  8. Patch SKILL.md to add the Automatic Learning section
//     (idempotent strip-then-re-emit).
//  9. Add modernc.org/sqlite to go.mod when missing, then run
//     `go mod tidy` inside the target CLI directory.
//  10. Update printing_press_version in .printing-press.json so the
//     registry regen reflects the new template version.
//
// Per-CLI failures roll back every file written for that CLI from the
// in-memory snapshot and continue to the next CLI. Non-zero exit when
// any CLI errored, even with rollback applied.
//
// Idempotency is a hard requirement: a second sweep on the same input
// must produce zero textual diff. Tests under main_test.go enforce
// this contract for every patch function.
//
// GOPATH-mode: no module file lives in this directory tree. Invoke as
// `SWEEP_LIBRARY_ROOT=library GO111MODULE=off go run ./tools/sweep-learn-install`
// from the repo root, mirroring sweep-canonical.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// manifest mirrors the subset of .printing-press.json fields the sweep
// reads. Anything else in the file is preserved verbatim via the raw
// JSON pass-through performed by updatePrintingPressVersion.
type manifest struct {
	APIName              string `json:"api_name"`
	CLIName              string `json:"cli_name"`
	OwnerName            string `json:"owner_name"`
	PrintingPressVersion string `json:"printing_press_version"`
}

// sqliteVersion is the modernc.org/sqlite version every learn-enabled
// CLI ships with. Pinned to match the generator's go.mod.tmpl. Keep
// in sync with cli-printing-press templates when the upstream bump
// lands.
const sqliteVersion = "v1.37.0"

// learnPressVersion is the printing_press_version stamp the sweep
// writes back to each CLI's manifest after a successful patch. Marks
// the on-disk artifact as carrying the learn-loop retrofit so the
// registry regen can report it correctly. Bump when the templates
// in tools/sweep-learn-install/templates/ change.
const learnPressVersion = "4.14.0+learn-loop"

func main() {
	libraryRoot := "library"
	if v := os.Getenv("SWEEP_LIBRARY_ROOT"); v != "" {
		libraryRoot = v
	}

	readmeOnly := false
	dryRun := false
	var onlySlug string
	for i := 1; i < len(os.Args); i++ {
		a := os.Args[i]
		switch {
		case a == "-readme-only" || a == "--readme-only":
			readmeOnly = true
		case a == "-dry-run" || a == "--dry-run":
			dryRun = true
		case strings.HasPrefix(a, "-only=") || strings.HasPrefix(a, "--only="):
			onlySlug = strings.TrimPrefix(strings.TrimPrefix(a, "--only="), "-only=")
		case a == "-only" || a == "--only":
			if i+1 < len(os.Args) {
				onlySlug = os.Args[i+1]
				i++
			}
		}
	}
	if !readmeOnly && strings.EqualFold(os.Getenv("SWEEP_README_ONLY"), "1") {
		readmeOnly = true
	}
	if !dryRun && strings.EqualFold(os.Getenv("SWEEP_DRY_RUN"), "1") {
		dryRun = true
	}

	cliDirs, err := findCLIDirs(libraryRoot)
	if err != nil {
		log.Fatalf("walking %s: %v", libraryRoot, err)
	}
	if len(cliDirs) == 0 {
		log.Fatalf("no per-CLI directories found under %s", libraryRoot)
	}

	if dryRun {
		fmt.Println("Running in -dry-run mode: no files will be written.")
	}
	if readmeOnly {
		fmt.Println("Running in -readme-only mode: only SKILL.md will be patched.")
	}

	var patched, upToDate, errored, skipped int
	for _, dir := range cliDirs {
		if onlySlug != "" && filepath.Base(dir) != onlySlug {
			continue
		}
		status, err := sweepCLI(dir, sweepOpts{
			ReadmeOnly: readmeOnly,
			DryRun:     dryRun,
		})
		switch {
		case err != nil:
			fmt.Printf("  ERROR %s: %v\n", dir, err)
			errored++
		case status == statusSkipped:
			fmt.Printf("  SKIPPED %s\n", dir)
			skipped++
		case status == statusUnchanged:
			upToDate++
		default:
			fmt.Printf("  SWEPT %s (%s)\n", dir, status)
			patched++
		}
	}

	fmt.Printf("\nSweep complete: %d patched, %d already up-to-date, %d errored, %d skipped\n", patched, upToDate, errored, skipped)
	if errored > 0 {
		os.Exit(1)
	}
}

// findCLIDirs returns library/<cat>/<api>/ directories in deterministic
// order.
func findCLIDirs(libraryRoot string) ([]string, error) {
	cats, err := os.ReadDir(libraryRoot)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, cat := range cats {
		if !cat.IsDir() {
			continue
		}
		catPath := filepath.Join(libraryRoot, cat.Name())
		apis, err := os.ReadDir(catPath)
		if err != nil {
			return nil, err
		}
		for _, api := range apis {
			if !api.IsDir() {
				continue
			}
			dirs = append(dirs, filepath.Join(catPath, api.Name()))
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

type sweepStatus string

const (
	statusUnchanged sweepStatus = "unchanged"
	statusPatched   sweepStatus = "patched"
	statusSkipped   sweepStatus = "skipped"
)

type sweepOpts struct {
	ReadmeOnly bool
	DryRun     bool
}

// snapshot captures one file's pre-sweep state for rollback. existed
// distinguishes "rewrite to original bytes" from "remove (didn't exist
// before)" so a partial-write rollback can correctly restore the
// initial directory listing.
type snapshot struct {
	path    string
	bytes   []byte
	existed bool
}

// sweepCLI applies the learn-install retrofit to one library/<cat>/<api>/
// directory. Atomic per CLI: any error after the first write rolls
// every touched file back from its in-memory snapshot before returning.
func sweepCLI(cliDir string, opts sweepOpts) (sweepStatus, error) {
	manifestPath := filepath.Join(cliDir, ".printing-press.json")
	mfData, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return statusSkipped, nil
		}
		return statusSkipped, fmt.Errorf("read manifest: %w", err)
	}
	var mf manifest
	if err := json.Unmarshal(mfData, &mf); err != nil {
		return statusSkipped, fmt.Errorf("parse manifest: %w", err)
	}
	if mf.CLIName == "" || mf.APIName == "" {
		return statusSkipped, fmt.Errorf("manifest missing cli_name or api_name")
	}

	if _, err := os.Stat(filepath.Join(cliDir, ".no-learn-sweep")); err == nil {
		return statusSkipped, nil
	}

	category := filepath.Base(filepath.Dir(cliDir))
	if category == "" {
		category = "other"
	}

	ctx := sweepCtx{
		CLIDir:     cliDir,
		CLIName:    mf.CLIName,
		APIName:    mf.APIName,
		Category:   category,
		OwnerName:  mf.OwnerName,
		ModulePath: fmt.Sprintf("github.com/mvanhorn/printing-press-library/library/%s/%s", category, mf.APIName),
	}

	// SKILL.md-only mode: skip all Go-source surgery. Used when
	// iterating on skill content separately from the code retrofit.
	if opts.ReadmeOnly {
		return sweepSkillOnly(ctx, opts)
	}

	rootPath := filepath.Join(cliDir, "internal", "cli", "root.go")
	rootData, err := os.ReadFile(rootPath)
	if err != nil {
		return statusSkipped, fmt.Errorf("read root.go: %w", err)
	}
	shape, err := detectRootShape(rootData)
	if err != nil {
		return statusSkipped, err
	}
	if shape == rootShapeLegacy {
		return statusSkipped, fmt.Errorf("legacy var rootCmd shape detected: manual review required (see printing-press-library AGENTS.md CLI root.go shape)")
	}
	ctx.RootShape = shape

	storePath := filepath.Join(cliDir, "internal", "store", "store.go")
	storeData, err := os.ReadFile(storePath)
	if err != nil {
		return statusSkipped, fmt.Errorf("read store.go: %w", err)
	}
	// Anchor presence is no longer a hard skip: patchStoreMigrations
	// falls through to bootstrap mode when the marker is missing,
	// seeding both the anchor and the learn-migrations block in one
	// atomic operation. Bootstrap itself refuses on store.go shapes it
	// can't safely splice (no migrations slice, multiple migrations
	// slices), and that refusal surfaces via the plan error below.

	planned, err := planSweep(ctx, rootData, storeData)
	if err != nil {
		return statusSkipped, fmt.Errorf("plan sweep: %w", err)
	}
	if !planned.HasChanges() {
		return statusUnchanged, nil
	}

	if opts.DryRun {
		fmt.Printf("  DRY-RUN %s: %s\n", cliDir, planned.Summary())
		return statusPatched, nil
	}

	snapshots, err := applySweep(ctx, planned)
	if err != nil {
		rollback(snapshots)
		return statusSkipped, err
	}
	return statusPatched, nil
}

// sweepSkillOnly handles the -readme-only branch: patch SKILL.md only,
// skipping all Go-source surgery, manifest writes, and go.mod tidies.
// Mirrors the sweep-canonical -readme-only contract for the parallel
// flag.
func sweepSkillOnly(ctx sweepCtx, opts sweepOpts) (sweepStatus, error) {
	skillPath := filepath.Join(ctx.CLIDir, "SKILL.md")
	before, err := os.ReadFile(skillPath)
	if err != nil {
		return statusSkipped, fmt.Errorf("read SKILL.md: %w", err)
	}
	after := patchSkillLearnSection(string(before), ctx)
	if after == string(before) {
		return statusUnchanged, nil
	}
	if opts.DryRun {
		fmt.Printf("  DRY-RUN %s: SKILL.md learning section\n", ctx.CLIDir)
		return statusPatched, nil
	}
	if err := os.WriteFile(skillPath, []byte(after), 0o644); err != nil {
		return statusSkipped, fmt.Errorf("write SKILL.md: %w", err)
	}
	return statusPatched, nil
}

// rollback restores every file in snapshots to its pre-sweep state.
// "Existed=false" snapshots cause the file to be removed (it didn't
// exist pre-sweep). A failure during restore prints a warning but
// continues with the rest so we never leave more files dirty than
// strictly necessary.
func rollback(snapshots []snapshot) {
	for _, s := range snapshots {
		if !s.existed {
			if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
				fmt.Printf("    WARN rollback remove %s failed: %v\n", s.path, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
			fmt.Printf("    WARN rollback mkdir %s failed: %v\n", s.path, err)
			continue
		}
		if err := os.WriteFile(s.path, s.bytes, 0o644); err != nil {
			fmt.Printf("    WARN rollback restore %s failed: %v\n", s.path, err)
		}
	}
}

// captureSnapshot reads a file (or marks it as nonexistent) so a
// later rollback can restore it. Used before every per-CLI write.
func captureSnapshot(path string) snapshot {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snapshot{path: path, existed: false}
		}
		// Treat read errors as nonexistent for rollback purposes; a
		// subsequent write that fails before producing a file is
		// equivalent to "remove it" anyway.
		return snapshot{path: path, existed: false}
	}
	return snapshot{path: path, bytes: b, existed: true}
}

// sweepCtx carries per-CLI identity through the patcher chain.
// Centralizes the module path computation so every emitter agrees on
// the canonical Go import path for the CLI's internal/learn
// subpackages.
type sweepCtx struct {
	CLIDir     string
	CLIName    string
	APIName    string
	Category   string
	OwnerName  string
	ModulePath string
	// RootShape carries the detected internal/cli/root.go shape so the
	// learn-package emitter can decide whether to ship the rootFlags
	// shim (factory-shape CLIs only). Zero value (rootShapeUnknown)
	// means "fall through to the canonical-shape emission path" which
	// is safe — only the factory branch conditionally emits the shim.
	RootShape rootShape
}

// sweepPlan accumulates the planned changes for one CLI before any
// file is written. Lets the dry-run path describe what would happen
// and lets applySweep restrict writes to changed paths only.
type sweepPlan struct {
	LearnFiles      map[string][]byte // relative path -> new content
	RootAfter       string
	RootChanged     bool
	StoreAfter      string
	StoreChanged    bool
	SkillAfter      string
	SkillChanged    bool
	ManifestAfter   []byte
	ManifestChanged bool
	GoModTidy       bool
}

func (p *sweepPlan) HasChanges() bool {
	return len(p.LearnFiles) > 0 || p.RootChanged || p.StoreChanged ||
		p.SkillChanged || p.ManifestChanged || p.GoModTidy
}

func (p *sweepPlan) Summary() string {
	parts := []string{}
	if n := len(p.LearnFiles); n > 0 {
		parts = append(parts, fmt.Sprintf("%d learn files", n))
	}
	if p.RootChanged {
		parts = append(parts, "root.go")
	}
	if p.StoreChanged {
		parts = append(parts, "store.go")
	}
	if p.SkillChanged {
		parts = append(parts, "SKILL.md")
	}
	if p.GoModTidy {
		parts = append(parts, "go.mod tidy")
	}
	if p.ManifestChanged {
		parts = append(parts, "manifest")
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}

// planSweep computes the full set of changes for one CLI without
// writing anything. The planning step is separated from application so
// a dry-run can describe the work and so applySweep can roll back
// cleanly on partial failure.
func planSweep(ctx sweepCtx, rootData, storeData []byte) (sweepPlan, error) {
	plan := sweepPlan{LearnFiles: map[string][]byte{}}

	learnFiles, err := renderLearnPackage(ctx)
	if err != nil {
		return plan, fmt.Errorf("render learn package: %w", err)
	}
	for rel, content := range learnFiles {
		full := filepath.Join(ctx.CLIDir, rel)
		existing, err := os.ReadFile(full)
		if err != nil || string(existing) != string(content) {
			plan.LearnFiles[rel] = content
		}
	}

	rootAfter, rootChanged, err := patchRootAST(string(rootData), ctx)
	if err != nil {
		return plan, fmt.Errorf("patch root.go: %w", err)
	}
	plan.RootAfter = rootAfter
	plan.RootChanged = rootChanged

	storeAfter, storeChanged, err := patchStoreMigrations(string(storeData), ctx)
	if err != nil {
		return plan, fmt.Errorf("patch store.go: %w", err)
	}
	plan.StoreAfter = storeAfter
	plan.StoreChanged = storeChanged

	skillPath := filepath.Join(ctx.CLIDir, "SKILL.md")
	if skillData, err := os.ReadFile(skillPath); err == nil {
		skillAfter := patchSkillLearnSection(string(skillData), ctx)
		plan.SkillAfter = skillAfter
		plan.SkillChanged = skillAfter != string(skillData)
	}

	goModPath := filepath.Join(ctx.CLIDir, "go.mod")
	if goModData, err := os.ReadFile(goModPath); err == nil {
		if !strings.Contains(string(goModData), "modernc.org/sqlite") {
			plan.GoModTidy = true
		}
	}

	manifestPath := filepath.Join(ctx.CLIDir, ".printing-press.json")
	mfData, err := os.ReadFile(manifestPath)
	if err == nil {
		newMf, changed, err := updatePrintingPressVersion(mfData, learnPressVersion)
		if err != nil {
			return plan, fmt.Errorf("update manifest: %w", err)
		}
		plan.ManifestAfter = newMf
		plan.ManifestChanged = changed
	}

	return plan, nil
}

// applySweep writes every planned change to disk, capturing snapshots
// before each write so the caller can roll back on partial failure.
func applySweep(ctx sweepCtx, plan sweepPlan) ([]snapshot, error) {
	var snaps []snapshot

	writeFile := func(path string, content []byte) error {
		snaps = append(snaps, captureSnapshot(path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
		}
		return os.WriteFile(path, content, 0o644)
	}

	for rel, content := range plan.LearnFiles {
		full := filepath.Join(ctx.CLIDir, rel)
		if err := writeFile(full, content); err != nil {
			return snaps, fmt.Errorf("write %s: %w", rel, err)
		}
	}
	if plan.RootChanged {
		if err := writeFile(filepath.Join(ctx.CLIDir, "internal", "cli", "root.go"), []byte(plan.RootAfter)); err != nil {
			return snaps, fmt.Errorf("write root.go: %w", err)
		}
	}
	if plan.StoreChanged {
		if err := writeFile(filepath.Join(ctx.CLIDir, "internal", "store", "store.go"), []byte(plan.StoreAfter)); err != nil {
			return snaps, fmt.Errorf("write store.go: %w", err)
		}
	}
	if plan.SkillChanged {
		if err := writeFile(filepath.Join(ctx.CLIDir, "SKILL.md"), []byte(plan.SkillAfter)); err != nil {
			return snaps, fmt.Errorf("write SKILL.md: %w", err)
		}
	}

	// go.mod / go.sum need an extra snapshot pair before tidy because
	// `go mod tidy` rewrites both. Captured even when tidy ultimately
	// no-ops so a later failure path can still restore.
	if plan.GoModTidy {
		goModPath := filepath.Join(ctx.CLIDir, "go.mod")
		goSumPath := filepath.Join(ctx.CLIDir, "go.sum")
		snaps = append(snaps, captureSnapshot(goModPath))
		snaps = append(snaps, captureSnapshot(goSumPath))
		if err := addSQLiteDep(goModPath); err != nil {
			return snaps, fmt.Errorf("add sqlite dep: %w", err)
		}
		if err := runGoModTidy(ctx.CLIDir); err != nil {
			return snaps, fmt.Errorf("go mod tidy: %w", err)
		}
	}
	if plan.ManifestChanged {
		if err := writeFile(filepath.Join(ctx.CLIDir, ".printing-press.json"), plan.ManifestAfter); err != nil {
			return snaps, fmt.Errorf("write manifest: %w", err)
		}
	}
	return snaps, nil
}

// runGoModTidy executes `go mod tidy` in the target CLI directory.
// Honors GOPRIVATE if the operator set one so private module pulls
// don't hit sumdb. Surfaces stderr in the returned error so a tidy
// failure tells the operator which module wedged.
func runGoModTidy(cliDir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = cliDir
	env := os.Environ()
	// GO111MODULE must be on inside the target CLI even when the
	// sweep tool itself runs in GOPATH mode (the per-CLI go.mod
	// makes it a module no matter what).
	env = append(env, "GO111MODULE=on")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
