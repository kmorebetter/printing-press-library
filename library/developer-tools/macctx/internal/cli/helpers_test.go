package cli

import (
	"reflect"
	"testing"
)

func TestPeekabooArgsAddsLocalFirstNoRemote(t *testing.T) {
	got := peekabooArgs("image", "--mode", "frontmost")
	want := []string{"image", "--mode", "frontmost", "--no-remote"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("peekabooArgs() = %#v, want %#v", got, want)
	}
}

func TestClipboardPreviewLimitsRunesAndNormalizesNewlines(t *testing.T) {
	preview, truncated := clipboardPreview("a\r\nbç😀d", 4)
	if !truncated {
		t.Fatal("expected truncated preview")
	}
	if preview != "a\nbç" {
		t.Fatalf("preview = %q, want %q", preview, "a\nbç")
	}
}

func TestClipboardTextFromOutputFindsNestedText(t *testing.T) {
	raw := `{"success":true,"data":{"clipboard":{"text":"secret-but-previewed"}}}`
	if got := clipboardTextFromOutput(raw); got != "secret-but-previewed" {
		t.Fatalf("clipboardTextFromOutput() = %q", got)
	}
}

func TestActiveAppFromJSON(t *testing.T) {
	raw := `{"success":true,"data":{"count":2,"apps":[{"name":"Finder","pid":1,"is_active":false},{"name":"Terminal","pid":2,"bundle_id":"com.apple.Terminal","is_active":true}]}}`
	got, err := activeAppFromJSON(raw)
	if err != nil {
		t.Fatalf("activeAppFromJSON returned error: %v", err)
	}
	if got.Name != "Terminal" || got.PID != 2 || got.BundleID != "com.apple.Terminal" {
		t.Fatalf("active app = %#v", got)
	}
}

func TestAddFlagSkipsEmptyValues(t *testing.T) {
	got := addFlag([]string{"see"}, "--app", "")
	if !reflect.DeepEqual(got, []string{"see"}) {
		t.Fatalf("empty flag was added: %#v", got)
	}
	got = addFlag(got, "--app", "Safari")
	want := []string{"see", "--app", "Safari"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("addFlag() = %#v, want %#v", got, want)
	}
}

func TestShellQuoteCommand(t *testing.T) {
	got := shellQuoteCommand([]string{"peekaboo", "type", "hello world", "--no-remote"})
	want := "peekaboo type 'hello world' --no-remote"
	if got != want {
		t.Fatalf("shellQuoteCommand() = %q, want %q", got, want)
	}
}

func TestActionDryRunDoesNotRequirePeekaboo(t *testing.T) {
	plan := actionPlan{Command: "peekaboo", Args: []string{"click", "--on", "B3", "--no-remote"}, DryRun: true}
	if err := runOrDescribeAction(plan, false, true); err != nil {
		t.Fatalf("dry-run action returned error: %v", err)
	}
}
