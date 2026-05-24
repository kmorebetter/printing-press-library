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
