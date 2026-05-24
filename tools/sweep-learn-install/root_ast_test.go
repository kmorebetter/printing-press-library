package main

import (
	"strings"
	"testing"
)

// canonicalRootFlagsShape mirrors the rootFlags-struct shape every
// newer printed CLI ships. The sweep operates on this shape.
const canonicalRootFlagsShape = `package cli

import (
	"context"
	"github.com/spf13/cobra"
)

type rootFlags struct {
	OutputJSON bool
	Verbose    bool
}

func Execute() error {
	var flags rootFlags
	rootCmd := &cobra.Command{
		Use: "demo-pp-cli",
	}
	rootCmd.PersistentFlags().BoolVar(&flags.OutputJSON, "json", false, "json output")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		_ = cmd
		_ = context.TODO()
		return nil
	}
	rootCmd.AddCommand(newResourceCmd(&flags))
	rootCmd.AddCommand(newSyncCmd(&flags))
	return rootCmd.Execute()
}
`

// legacyRootShape mirrors the agent-capture / instacart shape:
// package-global rootCmd with no rootFlags struct. The sweep refuses
// to patch this.
const legacyRootShape = `package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use: "agent-capture",
}

func Execute() error {
	return rootCmd.Execute()
}
`

func TestDetectRootShape(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want rootShape
	}{
		{"canonical-rootFlags-struct", canonicalRootFlagsShape, rootShapeFlagsStruct},
		{"legacy-var-rootCmd", legacyRootShape, rootShapeLegacy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := detectRootShape([]byte(tc.src))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got shape %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPatchRootAST_InjectsAllPieces(t *testing.T) {
	ctx := sweepCtx{CLIName: "demo-pp-cli", APIName: "demo"}
	got, changed, err := patchRootAST(canonicalRootFlagsShape, ctx)
	if err != nil {
		t.Fatalf("patchRootAST: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true on first run")
	}
	expectations := []string{
		"noLearn bool",
		`BoolVar(&flags.noLearn, "no-learn"`,
		"learnCfg := newLearnConfig()",
		"rootCmd.AddCommand(newTeachCmd(&flags, learnCfg))",
		"rootCmd.AddCommand(newRecallCmd(&flags, learnCfg))",
		"rootCmd.AddCommand(newLearningsCmd(&flags, learnCfg))",
		"rootCmd.AddCommand(newTeachPatternCmd(&flags))",
		"rootCmd.AddCommand(newTeachLookupCmd(&flags))",
		"learnHookSkipList",
		"func shouldSkipLearnHook(",
	}
	for _, want := range expectations {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in patched root.go; got:\n%s", want, got)
		}
	}
}

func TestPatchRootAST_Idempotent(t *testing.T) {
	ctx := sweepCtx{CLIName: "demo-pp-cli", APIName: "demo"}
	first, _, err := patchRootAST(canonicalRootFlagsShape, ctx)
	if err != nil {
		t.Fatalf("first patch: %v", err)
	}
	second, changed, err := patchRootAST(first, ctx)
	if err != nil {
		t.Fatalf("second patch: %v", err)
	}
	if changed {
		t.Error("expected changed=false on second run")
	}
	if second != first {
		t.Errorf("second run produced diff:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// ptrFlagsRootShape mirrors the company-goat / podcast-goat shape:
// rootFlags struct is declared at the top, but Execute() delegates to
// a newRootCmd(flags *rootFlags) factory. Inside newRootCmd the
// identifier `flags` is already `*rootFlags`, so passing `&flags` to
// the new<X>Cmd constructors yields a `**rootFlags` argument that
// fails to compile.
const ptrFlagsRootShape = `package cli

import (
	"github.com/spf13/cobra"
)

type rootFlags struct {
	OutputJSON bool
	Verbose    bool
}

func Execute() error {
	var flags rootFlags
	return newRootCmd(&flags).Execute()
}

func newRootCmd(flags *rootFlags) *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "demo-pp-cli",
	}
	rootCmd.PersistentFlags().BoolVar(&flags.OutputJSON, "json", false, "json output")
	rootCmd.AddCommand(newResourceCmd(flags))
	return rootCmd
}
`

// TestPatchRootAST_HostHasPtrFlags_EmitsPlainFlags regression-pins
// Bug B from the U14 pilot sweep findings: when the surrounding
// function signature is newRootCmd(flags *rootFlags), the injected
// AddCommand calls must pass `flags` (the existing pointer) rather
// than `&flags` (which would be **rootFlags).
func TestPatchRootAST_HostHasPtrFlags_EmitsPlainFlags(t *testing.T) {
	ctx := sweepCtx{CLIName: "demo-pp-cli", APIName: "demo"}
	got, changed, err := patchRootAST(ptrFlagsRootShape, ctx)
	if err != nil {
		t.Fatalf("patchRootAST: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true on first run")
	}
	// The new constructors must receive `flags`, not `&flags`.
	mustContain := []string{
		"rootCmd.AddCommand(newTeachCmd(flags, learnCfg))",
		"rootCmd.AddCommand(newRecallCmd(flags, learnCfg))",
		"rootCmd.AddCommand(newLearningsCmd(flags, learnCfg))",
		"rootCmd.AddCommand(newTeachPatternCmd(flags))",
		"rootCmd.AddCommand(newTeachLookupCmd(flags))",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in patched root.go (Bug B regression); got:\n%s", want, got)
		}
	}
	// Defense in depth: no `&flags` should appear in any of the
	// inserted constructor calls. The pre-existing `&flags` in
	// Execute() should still be there, so we scan specifically for
	// the new constructor patterns.
	for _, ctor := range []string{"newTeachCmd", "newRecallCmd", "newLearningsCmd", "newTeachPatternCmd", "newTeachLookupCmd"} {
		needle := ctor + "(&flags"
		if strings.Contains(got, needle) {
			t.Errorf("Bug B regression: %s called with &flags (would be **rootFlags):\n%s", needle, got)
		}
	}
}

// TestPatchRootAST_HostHasValueFlags_EmitsAddrOfFlags is the
// canonical case: rootFlags is a local value in Execute(), so the
// AddCommand calls must pass `&flags` (taking the address of the
// value). This is the path the canonicalRootFlagsShape fixture
// already exercises in TestPatchRootAST_InjectsAllPieces; the
// dedicated test name documents the contract explicitly.
func TestPatchRootAST_HostHasValueFlags_EmitsAddrOfFlags(t *testing.T) {
	ctx := sweepCtx{CLIName: "demo-pp-cli", APIName: "demo"}
	got, changed, err := patchRootAST(canonicalRootFlagsShape, ctx)
	if err != nil {
		t.Fatalf("patchRootAST: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true on first run")
	}
	mustContain := []string{
		"rootCmd.AddCommand(newTeachCmd(&flags, learnCfg))",
		"rootCmd.AddCommand(newRecallCmd(&flags, learnCfg))",
		"rootCmd.AddCommand(newLearningsCmd(&flags, learnCfg))",
		"rootCmd.AddCommand(newTeachPatternCmd(&flags))",
		"rootCmd.AddCommand(newTeachLookupCmd(&flags))",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in patched root.go (canonical value-flags shape); got:\n%s", want, got)
		}
	}
}

// TestPatchRootAST_HostHasPtrFlags_Idempotent asserts the ptr-flags
// shape is also idempotent: a second run produces zero diff.
func TestPatchRootAST_HostHasPtrFlags_Idempotent(t *testing.T) {
	ctx := sweepCtx{CLIName: "demo-pp-cli", APIName: "demo"}
	first, _, err := patchRootAST(ptrFlagsRootShape, ctx)
	if err != nil {
		t.Fatalf("first patch: %v", err)
	}
	second, changed, err := patchRootAST(first, ctx)
	if err != nil {
		t.Fatalf("second patch: %v", err)
	}
	if changed {
		t.Error("expected changed=false on second run")
	}
	if second != first {
		t.Errorf("second run produced diff on ptr-flags shape:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestPatchRootAST_RefusesLegacyShape(t *testing.T) {
	// Shape detection runs upstream in sweepCLI; patchRootAST itself
	// is exercised here against the canonical shape only. This test
	// exists so a future contributor accidentally relaxing the shape
	// gate notices: the legacy fixture must report
	// rootShapeLegacy from detectRootShape.
	shape, err := detectRootShape([]byte(legacyRootShape))
	if err != nil {
		t.Fatalf("detectRootShape: %v", err)
	}
	if shape != rootShapeLegacy {
		t.Errorf("expected legacy shape detection; got %d", shape)
	}
}

// factoryRootShape mirrors the instacart shape: a top-level
// `func Root() *cobra.Command` factory that constructs the command
// externally with no rootFlags struct. The sweep ships a rootFlags
// shim alongside (templates/cli/learn_root_shim.go.tmpl) and splices
// the learn wiring into the factory body just before `return root`.
const factoryRootShape = `package cli

import (
	"github.com/spf13/cobra"
)

var Version = "1.0.0"

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:     "demo",
		Short:   "Demo CLI for factory-shape sweep tests.",
		Version: Version,
	}

	root.PersistentFlags().Bool("json", false, "Machine-readable JSON output")
	root.PersistentFlags().Bool("dry-run", false, "Don't make network calls")

	root.AddCommand(
		newDoctorCmd(),
		newAuthCmd(),
	)

	return root
}
`

func TestDetectRootShape_FactoryRecognized(t *testing.T) {
	shape, err := detectRootShape([]byte(factoryRootShape))
	if err != nil {
		t.Fatalf("detectRootShape: %v", err)
	}
	if shape != rootShapeFactory {
		t.Errorf("expected rootShapeFactory; got %d", shape)
	}
}

// TestPatchRootAST_FactoryShape_InjectsLearnInit asserts the factory-
// shape patcher splices the canonical learn surface into the factory
// body. The injection lands BEFORE `return root` and uses the same
// identifier the factory returns (`root` here) for all AddCommand
// and PersistentFlags calls so the splice never has to know about
// the surrounding cobra command's name.
func TestPatchRootAST_FactoryShape_InjectsLearnInit(t *testing.T) {
	ctx := sweepCtx{CLIName: "demo-pp-cli", APIName: "demo"}
	got, changed, err := patchRootAST(factoryRootShape, ctx)
	if err != nil {
		t.Fatalf("patchRootAST: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true on first run")
	}
	expectations := []string{
		"learnCfg := newLearnConfig()",
		"var learnFlags rootFlags",
		`root.PersistentFlags().BoolVar(&learnFlags.noLearn, "no-learn"`,
		"root.AddCommand(newTeachCmd(&learnFlags, learnCfg))",
		"root.AddCommand(newRecallCmd(&learnFlags, learnCfg))",
		"root.AddCommand(newLearningsCmd(&learnFlags, learnCfg))",
		"root.AddCommand(newTeachPatternCmd(&learnFlags))",
		"root.AddCommand(newTeachLookupCmd(&learnFlags))",
		"learnHookSkipList",
		"func shouldSkipLearnHook(",
	}
	for _, want := range expectations {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in factory-shape patched root.go; got:\n%s", want, got)
		}
	}
	// The injection must land BEFORE the final return root.
	returnIdx := strings.LastIndex(got, "return root")
	teachIdx := strings.Index(got, "newTeachCmd(")
	if returnIdx < 0 || teachIdx < 0 || teachIdx > returnIdx {
		t.Errorf("expected newTeachCmd(...) BEFORE `return root`; teach@%d return@%d", teachIdx, returnIdx)
	}
	// The shim is emitted as a separate file by learn_files.go; this
	// patcher must NOT add `type rootFlags struct {` to root.go itself
	// (that would clash with the shim's declaration).
	if strings.Contains(got, "type rootFlags struct {") {
		t.Error("factory-shape patcher must NOT inject `type rootFlags struct` into root.go; the shim file owns it")
	}
}

// TestPatchRootAST_FactoryShape_Idempotent runs the patcher twice on
// the factory shape and asserts the second run reports changed=false
// with zero textual diff. Idempotency is the binding contract for
// every sweep emitter.
func TestPatchRootAST_FactoryShape_Idempotent(t *testing.T) {
	ctx := sweepCtx{CLIName: "demo-pp-cli", APIName: "demo"}
	first, _, err := patchRootAST(factoryRootShape, ctx)
	if err != nil {
		t.Fatalf("first patch: %v", err)
	}
	second, changed, err := patchRootAST(first, ctx)
	if err != nil {
		t.Fatalf("second patch: %v", err)
	}
	if changed {
		t.Error("expected changed=false on second run")
	}
	if second != first {
		t.Errorf("second run produced diff on factory shape:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestPatchRootAST_FactoryShape_PreservesExistingAddCommands asserts
// the pre-existing AddCommand(...) calls in the factory body
// (newDoctorCmd, newAuthCmd in the fixture) are still present after
// the patch. The splice lands BEFORE the return, after every
// pre-existing AddCommand, so existing wiring is preserved verbatim.
func TestPatchRootAST_FactoryShape_PreservesExistingAddCommands(t *testing.T) {
	ctx := sweepCtx{CLIName: "demo-pp-cli", APIName: "demo"}
	got, _, err := patchRootAST(factoryRootShape, ctx)
	if err != nil {
		t.Fatalf("patchRootAST: %v", err)
	}
	for _, want := range []string{"newDoctorCmd()", "newAuthCmd()"} {
		if !strings.Contains(got, want) {
			t.Errorf("factory-shape patch dropped pre-existing %q; got:\n%s", want, got)
		}
	}
	// Pre-existing PersistentFlags must also survive.
	for _, want := range []string{
		`root.PersistentFlags().Bool("json"`,
		`root.PersistentFlags().Bool("dry-run"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("factory-shape patch dropped pre-existing %q; got:\n%s", want, got)
		}
	}
}

// factoryRootShapeAltIdent mirrors the factory shape but uses
// `rootCmd` as the local identifier instead of `root`. The patcher
// must detect the actual return identifier via AST and use that
// same name for the splice.
const factoryRootShapeAltIdent = `package cli

import (
	"github.com/spf13/cobra"
)

func Root() *cobra.Command {
	rootCmd := &cobra.Command{Use: "demo"}
	rootCmd.AddCommand(newDoctorCmd())
	return rootCmd
}
`

func TestPatchRootAST_FactoryShape_DetectsReturnIdent(t *testing.T) {
	ctx := sweepCtx{CLIName: "demo-pp-cli", APIName: "demo"}
	got, changed, err := patchRootAST(factoryRootShapeAltIdent, ctx)
	if err != nil {
		t.Fatalf("patchRootAST: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	// All splice sites must use `rootCmd` (matching the factory's
	// returned identifier), not the hard-coded `root`.
	mustContain := []string{
		"rootCmd.AddCommand(newTeachCmd(&learnFlags, learnCfg))",
		`rootCmd.PersistentFlags().BoolVar(&learnFlags.noLearn`,
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in patched root.go; got:\n%s", want, got)
		}
	}
}
