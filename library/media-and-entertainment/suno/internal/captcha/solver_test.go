package captcha

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// readinessFake reports hcaptcha not-ready for notReadyLeft checks, then ready;
// the solve JS returns token.
type readinessFake struct {
	notReadyLeft int
	token        string
	readyChecks  int
}

func (f *readinessFake) setCookies(context.Context, []CDPCookie) error { return nil }
func (f *readinessFake) navigate(context.Context) error                { return nil }
func (f *readinessFake) showOnScreen(context.Context) error            { return nil }
func (f *readinessFake) close()                                        {}
func (f *readinessFake) evaluate(_ context.Context, js string) (string, error) {
	if strings.Contains(js, "hcaptcha.render === 'function'") {
		f.readyChecks++
		if f.notReadyLeft > 0 {
			f.notReadyLeft--
			return "waiting", nil
		}
		return "ready", nil
	}
	return f.token, nil
}

func newReadinessSolver(fb *readinessFake) *solver {
	return &solver{
		open: func(context.Context, Options, bool) (browser, error) { return fb, nil },
		seed: func(context.Context) ([]CDPCookie, error) { return nil, nil },
	}
}

func TestSolve_WaitsForHCaptchaThenSolves(t *testing.T) {
	fb := &readinessFake{notReadyLeft: 2, token: "P1_after_load"}
	tok, err := newReadinessSolver(fb).Solve(context.Background(), Options{Profile: "default", Seeded: true})
	if err != nil {
		t.Fatal(err)
	}
	if tok != "P1_after_load" {
		t.Fatalf("token=%q", tok)
	}
	if fb.readyChecks < 3 {
		t.Fatalf("expected >=3 readiness checks (2 waiting + 1 ready), got %d", fb.readyChecks)
	}
}

func TestSolve_HCaptchaNeverLoads_Errors(t *testing.T) {
	fb := &readinessFake{notReadyLeft: 1 << 30, token: "x"}
	_, err := newReadinessSolver(fb).Solve(context.Background(),
		Options{Profile: "default", Seeded: true, Timeout: 300 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "never finished loading") {
		t.Fatalf("want 'never finished loading' error, got %v", err)
	}
}

// fakeBrowser drives solver branch tests without real Chrome.
type fakeBrowser struct {
	evalResults []string // returned in sequence per evaluate call
	calls       int
	shown       bool
	cookiesSet  bool
}

func (f *fakeBrowser) setCookies(_ context.Context, _ []CDPCookie) error {
	f.cookiesSet = true
	return nil
}
func (f *fakeBrowser) navigate(_ context.Context) error { return nil }
func (f *fakeBrowser) evaluate(_ context.Context, js string) (string, error) {
	// The readiness poll runs before the solve JS; report ready immediately so
	// these branch tests exercise the solve path, not the load wait.
	if strings.Contains(js, "hcaptcha.render === 'function'") {
		return "ready", nil
	}
	r := f.evalResults[f.calls]
	f.calls++
	return r, nil
}
func (f *fakeBrowser) showOnScreen(_ context.Context) error { f.shown = true; return nil }
func (f *fakeBrowser) close()                               {}

func newTestSolver(fb *fakeBrowser, seed SeedFunc) *solver {
	return &solver{
		open: func(_ context.Context, _ Options, _ bool) (browser, error) { return fb, nil },
		seed: seed,
	}
}

func TestSolve_InvisibleSuccess(t *testing.T) {
	fb := &fakeBrowser{evalResults: []string{"P1_goodtoken"}}
	s := newTestSolver(fb, func(context.Context) ([]CDPCookie, error) { return nil, nil })
	tok, err := s.Solve(context.Background(), Options{Profile: "default", Seeded: true, Interactive: true})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if tok != "P1_goodtoken" {
		t.Fatalf("token=%q", tok)
	}
	if fb.shown {
		t.Fatal("should not have shown window on invisible success")
	}
}

func TestSolve_SeedsWhenUnseeded(t *testing.T) {
	fb := &fakeBrowser{evalResults: []string{"P1_tok"}}
	seeded := false
	s := newTestSolver(fb, func(context.Context) ([]CDPCookie, error) {
		seeded = true
		return []CDPCookie{{Name: "__client", Value: "x", Domain: ".suno.com", Path: "/"}}, nil
	})
	_, err := s.Solve(context.Background(), Options{Profile: "default", Seeded: false, Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	if !seeded || !fb.cookiesSet {
		t.Fatal("unseeded profile must seed cookies from browser")
	}
}

func TestSolve_InteractiveNeeded_NoInput_ReturnsErr(t *testing.T) {
	fb := &fakeBrowser{evalResults: []string{""}}
	s := newTestSolver(fb, func(context.Context) ([]CDPCookie, error) { return nil, nil })
	_, err := s.Solve(context.Background(), Options{Profile: "default", Seeded: true, Interactive: false})
	if !errors.Is(err, ErrInteractiveRequired) {
		t.Fatalf("want ErrInteractiveRequired, got %v", err)
	}
	if fb.shown {
		t.Fatal("must NOT show window under --no-input")
	}
}

func TestSolve_InteractiveFallback_ShowsAndRetries(t *testing.T) {
	fb := &fakeBrowser{evalResults: []string{"", "P1_after_manual"}}
	s := newTestSolver(fb, func(context.Context) ([]CDPCookie, error) { return nil, nil })
	tok, err := s.Solve(context.Background(), Options{Profile: "default", Seeded: true, Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	if tok != "P1_after_manual" {
		t.Fatalf("token=%q", tok)
	}
	if !fb.shown {
		t.Fatal("interactive fallback must show the window")
	}
}
