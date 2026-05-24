// root_ast.go injects the learn-loop wiring into a CLI's
// internal/cli/root.go. Operates on the canonical rootFlags-struct
// shape (per printing-press-library/AGENTS.md "CLI root.go shape").
// The legacy `var rootCmd` package-global shape is refused with an
// error so the sweep does not silently no-op or produce a broken
// patch.
//
// Four pieces are injected:
//
//  1. A persistent `noLearn` bool field on the rootFlags struct.
//     Matches the canonical generator template's field name (lowercase
//     n) so the emitted internal/cli/teach.go can reference
//     `flags.noLearn` without a separate field-name patch.
//  2. The cobra BoolVar binding for `--no-learn` on the persistent
//     flag set.
//  3. The five teach/recall/learnings/teach-pattern/teach-lookup
//     AddCommand registrations alongside a `learnCfg := newLearnConfig()`
//     declaration. teach.go's command constructors take both
//     `*rootFlags` and `*entities.Config` per the canonical generator
//     emission; the declaration sits adjacent to the AddCommand calls
//     so the variable's scope is the local one Execute's `var flags`
//     creates.
//  4. The `learnHookSkipList` map + `shouldSkipLearnHook` helper.
//     The skip list names framework commands that must bypass the
//     PersistentPreRunE learn-init hook (auth, doctor, version, help,
//     etc.); the helper is the one site consumers (today: tests; in
//     future the generator-emitted PreRunE) consult it from.
//
// Idempotency: a second run with the same input produces zero diff.
// Each injection probes for its own canonical marker before adding
// and is a no-op when the marker is already present.

package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

type rootShape int

const (
	rootShapeUnknown rootShape = iota
	// rootShapeFlagsStruct is the canonical shape: a rootFlags type
	// + Execute() with a local rootCmd binding + addPersistentFlags
	// against that local. The generator emits this for every new
	// CLI; the sweep retrofits learn wiring into it.
	rootShapeFlagsStruct
	// rootShapeLegacy is the agent-capture / instacart shape: a
	// package-global var rootCmd with no rootFlags struct. The AST
	// sweep refuses to patch this shape and reports it to the
	// operator for manual review.
	rootShapeLegacy
)

// detectRootShape parses root.go and decides which shape it carries.
// Returns rootShapeUnknown when the file doesn't even parse so the
// caller surfaces a clear error rather than silently no-oping.
//
// Three root shapes are observed in the published library; only the
// first two are auto-supported by this sweep:
//
//  1. rootShapeFlagsStruct — canonical shape every newer CLI ships:
//     `type rootFlags struct{}` + `Execute()` declaring
//     `var flags rootFlags` locally OR
//     `func newRootCmd(flags *rootFlags) *cobra.Command`. The AST
//     patcher detects parameter-vs-local in
//     flagsExprForAddCommands and emits the right flags expression
//     for each constructor call.
//  2. rootShapeLegacy — agent-capture / instacart-style
//     package-global `var rootCmd` with no rootFlags struct. The
//     sweep refuses these.
//  3. Factory shape — `func Root() *cobra.Command` (or similar)
//     that constructs the command externally with no rootFlags
//     struct in scope. instacart ships this. detectRootShape
//     surfaces it via a distinct refusal message so a future
//     maintainer can tell it apart from the totally-unknown case
//     and route the CLI through a manual retrofit; the auto-sweep
//     intentionally does not patch this shape.
func detectRootShape(src []byte) (rootShape, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "root.go", src, parser.ParseComments)
	if err != nil {
		return rootShapeUnknown, fmt.Errorf("parse root.go: %w", err)
	}

	hasRootFlagsType := false
	hasPackageRootCmd := false
	hasRootFactory := false
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name != nil && s.Name.Name == "rootFlags" {
						hasRootFlagsType = true
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name == "rootCmd" {
							hasPackageRootCmd = true
						}
					}
				}
			}
		case *ast.FuncDecl:
			// Detect the third shape: an exported, package-level
			// factory like `func Root() *cobra.Command`. Names like
			// `RootCmd` are also treated as factories. Only fires
			// when there is no rootFlags struct, so the canonical
			// shape (which can also expose a RootCmd helper)
			// continues to land in rootShapeFlagsStruct.
			if d.Recv != nil {
				continue
			}
			if d.Name == nil {
				continue
			}
			n := d.Name.Name
			if n == "Root" || n == "RootCmd" {
				hasRootFactory = true
			}
		}
	}

	if hasRootFlagsType {
		return rootShapeFlagsStruct, nil
	}
	if hasPackageRootCmd {
		return rootShapeLegacy, nil
	}
	if hasRootFactory {
		// Recognized but unsupported: instacart's `func Root() *cobra.Command`
		// pattern carries no rootFlags struct and constructs the
		// command externally. The auto-sweep does not patch this
		// shape — see tools/sweep-learn-install/README.md
		// "Recognized but unsupported root shapes" for the manual
		// retrofit path.
		return rootShapeUnknown, fmt.Errorf("root.go uses the `func Root() *cobra.Command` factory shape with no rootFlags struct (recognized but unsupported by auto-sweep; manual retrofit required, see tools/sweep-learn-install/README.md)")
	}
	return rootShapeUnknown, fmt.Errorf("root.go shape unrecognized (no rootFlags type, no var rootCmd, no Root()/RootCmd() factory)")
}

// patchRootAST applies the four injections (flag field, BoolVar binding,
// learnCfg + AddCommands, skip-list) to a canonical-shape root.go.
// Returns the new source (still go-fmt-clean because edits operate on
// whole lines or self-contained blocks) plus a changed boolean.
func patchRootAST(src string, ctx sweepCtx) (string, bool, error) {
	out := src
	changed := false

	if added, ok := injectNoLearnFlagField(out); ok {
		out = added
		changed = true
	}
	if added, ok := injectNoLearnPersistentFlag(out); ok {
		out = added
		changed = true
	}
	if added, ok := injectLearnAddCommands(out, ctx); ok {
		out = added
		changed = true
	}
	if added, ok := injectLearnHookSkipList(out); ok {
		out = added
		changed = true
	}
	if changed {
		// Run gofmt over the final source so injection seams (extra
		// blank lines, slightly off-spec indentation) settle into a
		// canonical shape. If gofmt fails (a non-canonical input
		// would surface as a compile error downstream), pass the
		// raw output through and let the caller see it.
		if formatted, err := format.Source([]byte(out)); err == nil {
			out = string(formatted)
		}
	}
	return out, changed, nil
}

// injectNoLearnFlagField adds a noLearn bool to the rootFlags struct.
// Lowercase name to match the generator template's emission so the
// teach.go template (which references flags.noLearn) compiles without
// a second rewrite. Idempotent: skipped when the field is already
// present.
func injectNoLearnFlagField(src string) (string, bool) {
	if strings.Contains(src, "noLearn bool") {
		return src, false
	}
	// Find the rootFlags struct opening brace and inject a noLearn
	// field right before the closing brace. Conservative: matches
	// the literal `type rootFlags struct {` header so we don't
	// accidentally patch a similarly-named local.
	const header = "type rootFlags struct {"
	idx := strings.Index(src, header)
	if idx < 0 {
		return src, false
	}
	openBrace := idx + len(header) - 1
	depth := 0
	closeIdx := -1
	for i := openBrace; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closeIdx = i
			}
		}
		if closeIdx >= 0 {
			break
		}
	}
	if closeIdx < 0 {
		return src, false
	}
	// Walk back to the line start so we can insert a properly
	// indented line just before the closing brace.
	lineStart := closeIdx
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	insertion := "\t// noLearn suppresses self-learning loop seed/extract/recall side\n" +
		"\t// effects when true. Set by the persistent --no-learn flag.\n" +
		"\tnoLearn bool\n"
	return src[:lineStart] + insertion + src[lineStart:], true
}

// injectNoLearnPersistentFlag adds the cobra BoolVar binding for
// --no-learn. Idempotent: skipped when the binding is already present.
func injectNoLearnPersistentFlag(src string) (string, bool) {
	if strings.Contains(src, `BoolVar(&flags.noLearn, "no-learn"`) {
		return src, false
	}
	// Find the last line in Execute() that calls rootCmd.PersistentFlags()
	// and inject immediately after the end of that line. Line-scope
	// matching avoids splitting a chained method call (the `()` in
	// `PersistentFlags()` would otherwise satisfy the first depth=0
	// drop and yield a splice point inside the statement).
	lineEnd := lastLineEndContaining(src, "rootCmd.PersistentFlags()")
	if lineEnd < 0 {
		return src, false
	}
	insertion := "\trootCmd.PersistentFlags().BoolVar(&flags.noLearn, \"no-learn\", false, \"Disable the teach/recall learning loop for this invocation\")\n"
	return src[:lineEnd] + insertion + src[lineEnd:], true
}

// lastLineEndContaining returns the byte offset just past the newline
// of the last line that contains needle. -1 when none. Used by
// inject* helpers that want to splice immediately after a stable
// per-line anchor.
func lastLineEndContaining(src, needle string) int {
	idx := strings.LastIndex(src, needle)
	if idx < 0 {
		return -1
	}
	lineEnd := strings.Index(src[idx:], "\n")
	if lineEnd < 0 {
		return len(src)
	}
	return idx + lineEnd + 1
}

// injectLearnAddCommands wires the five teach/recall/learnings/
// teach-pattern/teach-lookup cobra commands into root.go. The sweep
// emits newTeachCmd / newRecallCmd / newLearningsCmd / newTeachPatternCmd
// / newTeachLookupCmd constructors in internal/cli/teach.go (Gap 2's
// new emission). newLearnConfig is emitted in internal/cli/learn_init.go
// alongside; this injection declares the local learnCfg variable
// teach/recall/learnings consume.
//
// The constructors expect `*rootFlags`, so the argument expression
// depends on the surrounding scope's `flags` identifier:
//
//   - When `flags` is a local value (`var flags rootFlags` inside
//     Execute), we pass `&flags`.
//   - When `flags` is already a pointer parameter
//     (`func newRootCmd(flags *rootFlags)` — the company-goat /
//     podcast-goat shape), we pass `flags` directly. Passing `&flags`
//     there yields `**rootFlags` and fails to compile.
//
// Idempotent: skipped when newTeachCmd is already referenced.
func injectLearnAddCommands(src string, ctx sweepCtx) (string, bool) {
	if strings.Contains(src, "newTeachCmd(") {
		return src, false
	}
	// Anchor on the last line that calls rootCmd.AddCommand. Same
	// line-scoped splicing as injectNoLearnPersistentFlag to keep
	// each statement intact.
	lineEnd := lastLineEndContaining(src, "rootCmd.AddCommand(")
	if lineEnd < 0 {
		return src, false
	}
	// Figure out the right flags expression for the surrounding scope.
	// If the AST scan can't decide, fall back to `&flags` (the canonical
	// shape that ships with newer generator emission) so the legacy
	// behavior is preserved when detection fails.
	flagsExpr := flagsExprForAddCommands(src, lineEnd)
	// learnCfg is built once and passed by pointer to each of teach,
	// recall, and learnings so they share configuration. The two
	// manual-install commands (teach-pattern, teach-lookup) take only
	// flags per the generator template.
	insertion := fmt.Sprintf("\tlearnCfg := newLearnConfig()\n"+
		"\trootCmd.AddCommand(newTeachCmd(%s, learnCfg))\n"+
		"\trootCmd.AddCommand(newRecallCmd(%s, learnCfg))\n"+
		"\trootCmd.AddCommand(newLearningsCmd(%s, learnCfg))\n"+
		"\trootCmd.AddCommand(newTeachPatternCmd(%s))\n"+
		"\trootCmd.AddCommand(newTeachLookupCmd(%s))\n",
		flagsExpr, flagsExpr, flagsExpr, flagsExpr, flagsExpr)
	return src[:lineEnd] + insertion + src[lineEnd:], true
}

// flagsExprForAddCommands returns the expression to use for passing
// the rootFlags pointer to the new<X>Cmd constructors at insertion
// offset `insertOffset`. Returns "&flags" if `flags` is in scope as
// a `rootFlags` value (the canonical newer-generator shape) or "flags"
// if it's in scope as `*rootFlags` (the older newRootCmd(flags
// *rootFlags) shape used by company-goat / podcast-goat).
//
// Falls back to "&flags" when the AST scan can't decide so a parser
// hiccup never silently breaks the canonical-shape case.
func flagsExprForAddCommands(src string, insertOffset int) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "root.go", src, parser.ParseComments)
	if err != nil {
		return "&flags"
	}
	// Find the function decl whose body brackets `insertOffset` and
	// look up the type of `flags` in that scope. Parameters win over
	// locals (the bug case has `flags *rootFlags` as a parameter and
	// no `var flags rootFlags` local at all).
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		start := fset.Position(fn.Body.Lbrace).Offset
		end := fset.Position(fn.Body.Rbrace).Offset
		if insertOffset < start || insertOffset > end {
			continue
		}
		// Parameter `flags`?
		if fn.Type != nil && fn.Type.Params != nil {
			for _, field := range fn.Type.Params.List {
				for _, name := range field.Names {
					if name.Name != "flags" {
						continue
					}
					if _, isStar := field.Type.(*ast.StarExpr); isStar {
						return "flags"
					}
					return "&flags"
				}
			}
		}
		// Local `var flags rootFlags` or `flags := rootFlags{}` inside
		// the function body. The canonical shape ships this form;
		// scan for it so we still return "&flags" when the parameter
		// scan was empty.
		hasLocalValue := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.DeclStmt:
				gd, ok := s.Decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					return true
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						if name.Name != "flags" {
							continue
						}
						// `var flags rootFlags` — value, not pointer.
						if _, isStar := vs.Type.(*ast.StarExpr); !isStar {
							hasLocalValue = true
						}
					}
				}
			}
			return true
		})
		if hasLocalValue {
			return "&flags"
		}
		// Found the enclosing function but couldn't decide; fall
		// through to default.
		return "&flags"
	}
	return "&flags"
}

// injectLearnHookSkipList adds the learnHookSkipList map and the
// shouldSkipLearnHook helper. The list names framework commands
// that must bypass any PersistentPreRunE learn-init hook (auth,
// doctor, version, help — commands that ship without a store). The
// helper is the one site consumers consult the list from. Mirrors
// the canonical generator template emission so the package keeps
// parity with fresh prints.
//
// Idempotent: skipped when learnHookSkipList already exists.
func injectLearnHookSkipList(src string) (string, bool) {
	if strings.Contains(src, "learnHookSkipList") {
		return src, false
	}
	// Append at file end so we don't disturb any existing top-level
	// declarations. The block carries its own doc comment so a
	// downstream reader knows what it's for without grepping.
	insertion := "\n// learnHookSkipList enumerates framework command names that any\n" +
		"// future PersistentPreRunE recall hook must NOT trigger on. Today the\n" +
		"// teach/recall path is invoked explicitly by the agent, so there is\n" +
		"// no consumer of this list at runtime; the skip-list ships in v1 as\n" +
		"// forward-looking framework so a later auto-recall hook can consult\n" +
		"// it without re-deriving the set in every PR.\n" +
		"//\n" +
		"// Names match the cobra Use: field. Aliases are matched as-is.\n" +
		"var learnHookSkipList = map[string]struct{}{\n" +
		"\t\"auth\":          {},\n" +
		"\t\"doctor\":        {},\n" +
		"\t\"help\":          {},\n" +
		"\t\"sync\":          {},\n" +
		"\t\"profile\":       {},\n" +
		"\t\"feedback\":      {},\n" +
		"\t\"which\":         {},\n" +
		"\t\"agent-context\": {},\n" +
		"\t\"completion\":    {},\n" +
		"\t\"version\":       {},\n" +
		"}\n" +
		"\n" +
		"// shouldSkipLearnHook reports whether a recall pre-run hook should\n" +
		"// short-circuit for cmdName. Used today only by unit tests asserting\n" +
		"// the contents of learnHookSkipList; reserved for a future\n" +
		"// PersistentPreRunE auto-recall integration.\n" +
		"func shouldSkipLearnHook(cmdName string) bool {\n" +
		"\t_, skip := learnHookSkipList[cmdName]\n" +
		"\treturn skip\n" +
		"}\n"
	out := src
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out + insertion, true
}
