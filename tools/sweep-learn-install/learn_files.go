// learn_files.go renders the internal/learn package files byte-for-
// byte identical to what cli-printing-press's generator emits when a
// spec opts into the self-learning loop.
//
// Parity strategy:
//
//   - The generator's learn templates are embedded into this binary
//     verbatim via go:embed (see templates.go). Embedding the same
//     source text the generator parses removes any chance of drift
//     between an inlined Go string literal and the real template.
//   - We parse each template with the same text/template funcs the
//     generator binds (currentYear, modulePath, kebab plus the
//     identity .Owner / .Name accessors). The funcs that involve
//     spec-derived shape (HasCostThrottling, EndpointTemplateVars,
//     etc.) are not referenced by the learn templates, so the small
//     subset suffices.
//   - We run go/format.Source over the rendered output, mirroring the
//     generator's normalizeRendered behavior for .go files. Without
//     this final pass, hand-aligned struct columns in the templates
//     would diff against the generator's own gofmt-aware emit path.
//
// The byte-for-byte parity test (learn_files_test.go) renders this
// tool's emission for the learn-loop-example fixture and diffs against
// the in-tree golden artifact. Zero textual diff is the contract.
//
// In addition to the internal/learn data layer, this file also
// renders the two internal/cli surface files that wire the learning
// commands into cobra: teach.go (cobra command constructors) and
// learn_init.go (newLearnConfig + initLearn shim). Those files live
// in the cli package (not internal/learn) so they can reference the
// rootFlags struct and the per-CLI helpers. The sweep emits them with
// a stub Learn config (no ticker patterns, no stopwords, no entity
// lookup seeds) so the package compiles even before the operator
// authors a per-CLI learn block. Operators add real Learn data by
// hand-editing learn_init.go later — the byte-for-byte parity check
// guards the stub shape against drift from the canonical generator
// template.

package main

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"path"
	"strconv"
	"strings"
	"text/template"
	"time"
)

//go:embed templates/learn/*.tmpl templates/learn_entities/*.tmpl templates/learn_lookups/*.tmpl templates/learn_patterns/*.tmpl templates/cli/*.tmpl
var learnTemplateFS embed.FS

// learnTemplatePaths maps each embedded template path to the
// CLI-relative output path the generator writes it to. Kept here so
// the sweep emits the same file set and ordering as the generator's
// renderLearnFiles in cli-printing-press/internal/generator/generator.go.
var learnTemplatePaths = map[string]string{
	"templates/learn/doc.go.tmpl":            "internal/learn/doc.go",
	"templates/learn/normalize.go.tmpl":      "internal/learn/normalize.go",
	"templates/learn/normalize_test.go.tmpl": "internal/learn/normalize_test.go",
	"templates/learn/match.go.tmpl":          "internal/learn/match.go",
	"templates/learn/match_test.go.tmpl":     "internal/learn/match_test.go",
	"templates/learn/recall.go.tmpl":         "internal/learn/recall.go",
	"templates/learn/recall_test.go.tmpl":    "internal/learn/recall_test.go",
	"templates/learn/teach.go.tmpl":          "internal/learn/teach.go",
	"templates/learn/teach_test.go.tmpl":     "internal/learn/teach_test.go",
	"templates/learn/teach_log.go.tmpl":      "internal/learn/teach_log.go",
	"templates/learn/teach_log_test.go.tmpl": "internal/learn/teach_log_test.go",
	"templates/learn/preseed.go.tmpl":        "internal/learn/preseed.go",
	"templates/learn/preseed_test.go.tmpl":   "internal/learn/preseed_test.go",

	"templates/learn_entities/config.go.tmpl":       "internal/learn/entities/config.go",
	"templates/learn_entities/config_test.go.tmpl":  "internal/learn/entities/config_test.go",
	"templates/learn_entities/extract.go.tmpl":      "internal/learn/entities/extract.go",
	"templates/learn_entities/extract_test.go.tmpl": "internal/learn/entities/extract_test.go",

	"templates/learn_lookups/store.go.tmpl":      "internal/learn/lookups/store.go",
	"templates/learn_lookups/store_test.go.tmpl": "internal/learn/lookups/store_test.go",
	"templates/learn_lookups/seeds.go.tmpl":      "internal/learn/lookups/seeds.go",
	"templates/learn_lookups/seeds_test.go.tmpl": "internal/learn/lookups/seeds_test.go",

	"templates/learn_patterns/doc.go.tmpl":          "internal/learn/patterns/doc.go",
	"templates/learn_patterns/store.go.tmpl":        "internal/learn/patterns/store.go",
	"templates/learn_patterns/store_test.go.tmpl":   "internal/learn/patterns/store_test.go",
	"templates/learn_patterns/extract.go.tmpl":      "internal/learn/patterns/extract.go",
	"templates/learn_patterns/extract_test.go.tmpl": "internal/learn/patterns/extract_test.go",
	"templates/learn_patterns/apply.go.tmpl":        "internal/learn/patterns/apply.go",
	"templates/learn_patterns/apply_test.go.tmpl":   "internal/learn/patterns/apply_test.go",

	// Cobra-surface emissions. These live in internal/cli/ (not the
	// internal/learn package) because they import cobra and reference
	// rootFlags. teach.go is identical to the generator's emission for
	// a learn-enabled spec; learn_init.go renders with a stub Learn
	// config (empty TickerPatterns/Stopwords/EntityLookupSeeds) so the
	// package compiles even before the operator authors per-CLI data.
	"templates/cli/teach.go.tmpl":      "internal/cli/teach.go",
	"templates/cli/learn_init.go.tmpl": "internal/cli/learn_init.go",
}

// factoryShapeShimTemplate is the embedded path for the rootFlags
// compatibility shim emitted only when the host root.go uses the
// factory shape (instacart's `func Root() *cobra.Command` form).
// Canonical-shape CLIs already declare a rootFlags struct in their
// own root.go; emitting the shim there would produce a duplicate-
// type compile error.
const (
	factoryShapeShimTemplate = "templates/cli/learn_root_shim.go.tmpl"
	factoryShapeShimOutput   = "internal/cli/learn_root_shim.go"
)

// stubLearnConfig is the renderData.Learn value for the empty-spec
// case: a learn-enabled CLI whose spec carries no TickerPatterns,
// Stopwords, or EntityLookupSeeds. With this shape, learn_init.go.tmpl
// renders the no-op newLearnConfig + initLearn shim documented in
// learn_init.go.tmpl's preamble. The byte-for-byte parity test for
// the stub case (TestEmitsLearnInitGo_StubDefaults) locks the shape.
type stubLearnConfig struct {
	TickerPatterns    []string
	Stopwords         []string
	EntityLookupSeeds map[string][]stubLookupSeed
}

// stubLookupSeed mirrors spec.LookupSeed for template iteration. Empty
// in the stub case; declared here so the template's
// `range $entries` does not panic on a nil interface.
type stubLookupSeed struct {
	Canonical string
	Aliases   []string
}

// renderData is the minimal subset of fields the learn templates
// reference. Mirrors the spec accessors the generator threads through
// .Owner / .Name; the rest of APISpec is not touched by these
// templates. Learn carries the cobra-surface templates'
// ticker-patterns / stopwords / entity-lookup-seeds inputs; the stub
// shape leaves all three empty so the canonical no-op output renders.
type renderData struct {
	Owner string
	Name  string
	Learn stubLearnConfig
}

// renderLearnPackage emits every learn-package file for one CLI and
// returns a path->content map ready for write. Module path and year
// land via the template funcs registered below.
//
// When ctx.RootShape is rootShapeFactory the function ALSO emits
// internal/cli/learn_root_shim.go — the tiny rootFlags struct
// teach.go references. Canonical-shape CLIs already declare their
// own rootFlags; emitting the shim there would clash.
func renderLearnPackage(ctx sweepCtx) (map[string][]byte, error) {
	out := make(map[string][]byte, len(learnTemplatePaths))
	for tmplPath, relOut := range learnTemplatePaths {
		content, err := renderLearnTemplate(tmplPath, ctx)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", tmplPath, err)
		}
		out[relOut] = content
	}
	if ctx.RootShape == rootShapeFactory {
		shim, err := renderLearnTemplate(factoryShapeShimTemplate, ctx)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", factoryShapeShimTemplate, err)
		}
		out[factoryShapeShimOutput] = shim
	}
	return out, nil
}

// renderLearnTemplate reads one embedded template, executes it
// against renderData{Owner, Name, Learn: stub}, gofmt's the result,
// and returns the final bytes — the same chain the generator runs for
// the matching template.
func renderLearnTemplate(tmplPath string, ctx sweepCtx) ([]byte, error) {
	raw, err := learnTemplateFS.ReadFile(tmplPath)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", tmplPath, err)
	}
	tmpl, err := template.New(path.Base(tmplPath)).Funcs(template.FuncMap{
		"currentYear": func() string { return strconv.Itoa(time.Now().Year()) },
		"modulePath":  func() string { return ctx.ModulePath },
		"kebab":       toKebab,
		"backtick":    func() string { return "`" },
		"envPrefix":   envPrefix,
	}).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", tmplPath, err)
	}
	var buf bytes.Buffer
	data := renderData{
		Owner: ctx.OwnerName,
		Name:  ctx.APIName,
		Learn: stubLearnConfig{
			// Stub shape: every field empty so the templates land on
			// the no-op branch. Operators populate by hand-editing
			// learn_init.go after the sweep.
			TickerPatterns:    nil,
			Stopwords:         nil,
			EntityLookupSeeds: nil,
		},
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute %s: %w", tmplPath, err)
	}
	rendered := bytes.TrimRight(buf.Bytes(), " \t\r\n")
	rendered = append(rendered, '\n')
	formatted, err := format.Source(rendered)
	if err != nil {
		// Mirror the generator's behavior: fall through with a stderr
		// warning rather than fail-hard, so a malformed template
		// surfaces as a compile error downstream.
		return rendered, nil
	}
	return formatted, nil
}

// toKebab mirrors the generator's kebab helper. Lowercases, replaces
// underscores / spaces / dots / slashes with hyphens, and folds
// repeated separators. The learn templates only call this on the
// per-CLI Name to compute a state-directory suffix, so the small
// implementation here suffices.
func toKebab(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			prevDash = false
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-':
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		case r == '_' || r == ' ' || r == '.' || r == '/':
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		default:
			// Drop punctuation the generator's toKebab also drops.
		}
	}
	return strings.Trim(b.String(), "-")
}

// envPrefix mirrors the generator's naming.EnvPrefix helper. Returns
// an ASCII-only shell-safe env-var prefix derived from the CLI name.
// Used by templates/cli/teach.go.tmpl for the {{envPrefix .Name}}_NO_LEARN
// constant.
func envPrefix(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "API"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "API_" + out
	}
	return out
}
