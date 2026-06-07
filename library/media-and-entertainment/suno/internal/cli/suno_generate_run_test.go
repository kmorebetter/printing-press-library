package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/suno/internal/captcha"
	"github.com/spf13/cobra"
)

type stubSolver struct {
	tok string
	err error
}

func (s stubSolver) Solve(_ context.Context, _ captcha.Options) (string, error) {
	return s.tok, s.err
}

func TestCaptchaGateAction(t *testing.T) {
	gateErr := errors.New(`generate returned http 422: {"detail":"token_validation_failed"}`)
	nullToken500 := errors.New("generate returned http 500: server_error")
	otherErr := errors.New("generate returned http 503: service unavailable")

	cases := []struct {
		name        string
		err         error
		tokenWasNil bool
		noCaptcha   bool
		want        captchaAction
	}{
		{"gate error, solver enabled -> solve", gateErr, false, false, captchaSolve},
		{"gate error, --no-captcha -> suppressed", gateErr, false, true, captchaSuppressed},
		{"null-token 500, solver enabled -> solve", nullToken500, true, false, captchaSolve},
		{"null-token 500, --no-captcha -> suppressed", nullToken500, true, true, captchaSuppressed},
		{"non-gate error -> proceed", otherErr, false, false, captchaProceed},
		{"non-gate error, --no-captcha -> proceed", otherErr, false, true, captchaProceed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := captchaGateAction(tc.err, tc.tokenWasNil, tc.noCaptcha); got != tc.want {
				t.Fatalf("captchaGateAction = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandleCaptchaGate_Success_ReturnsToken(t *testing.T) {
	prev := defaultSolver
	defaultSolver = stubSolver{tok: "P1_tok"}
	defer func() { defaultSolver = prev }()

	tok, err := handleCaptchaGate(context.Background(), "", true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if tok != "P1_tok" {
		t.Fatalf("token=%q", tok)
	}
}

func TestHandleCaptchaGate_InteractiveRequired_PropagatesSentinel(t *testing.T) {
	prev := defaultSolver
	defaultSolver = stubSolver{err: captcha.ErrInteractiveRequired}
	defer func() { defaultSolver = prev }()

	_, err := handleCaptchaGate(context.Background(), "", false)
	if !errors.Is(err, captcha.ErrInteractiveRequired) {
		t.Fatalf("want ErrInteractiveRequired propagated, got %v", err)
	}
}

func TestCaptchaGateFailure_AgentEmitsEnvelopeOnStdout(t *testing.T) {
	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	flags := &rootFlags{asJSON: true}

	err := captchaGateFailure(cmd, flags)
	if ExitCode(err) != 2 {
		t.Fatalf("exit code = %d, want 2", ExitCode(err))
	}
	var env map[string]any
	if jerr := json.Unmarshal(stdout.Bytes(), &env); jerr != nil {
		t.Fatalf("stdout not JSON: %q (%v)", stdout.String(), jerr)
	}
	if env["error_type"] != "captcha_required" {
		t.Fatalf("error_type = %v, want captcha_required", env["error_type"])
	}
	if env["retriable"] != true {
		t.Fatalf("retriable = %v, want true", env["retriable"])
	}
}

func TestCaptchaGateFailure_NonJSON_NoEnvelope(t *testing.T) {
	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	flags := &rootFlags{asJSON: false}

	_ = captchaGateFailure(cmd, flags)
	if stdout.Len() != 0 {
		t.Fatalf("non-JSON mode must not emit envelope to stdout, got %q", stdout.String())
	}
}
