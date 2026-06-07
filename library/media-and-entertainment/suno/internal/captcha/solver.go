// Copyright 2026 horknfbr. Licensed under Apache-2.0. See LICENSE.
//
// Solve orchestrator: ensure the dedicated Chrome -> (seed once) -> navigate ->
// invisible execute(); on challenge-expired, fall back to a visible manual
// solve ONLY when Options.Interactive is true. Under --no-input/--agent the
// visible window is never shown — ErrInteractiveRequired is returned instead.

package captcha

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// solver is the default Solver. Fields are function seams for testability.
type solver struct {
	open func(ctx context.Context, opts Options, visible bool) (browser, error)
	seed SeedFunc
}

// New returns the production Solver.
func New() Solver {
	return &solver{
		open: func(ctx context.Context, opts Options, visible bool) (browser, error) {
			return openBrowser(ctx, opts, visible)
		},
		seed: seedFromBrowser,
	}
}

// interactivePollInterval is how often the visible-fallback re-checks for a
// solved token.
const interactivePollInterval = 2 * time.Second

// hcaptchaLoadPollInterval is how often we re-check whether the hCaptcha API
// has finished loading on the page.
const hcaptchaLoadPollInterval = 250 * time.Millisecond

// hcaptchaReadyJS resolves to "ready" once the hCaptcha API is loaded and
// hcaptcha.render is callable, and "waiting" otherwise.
func hcaptchaReadyJS() string {
	return `(typeof hcaptcha !== 'undefined' && typeof hcaptcha.render === 'function') ? 'ready' : 'waiting'`
}

// waitForHCaptchaReady polls the page until the hCaptcha API is loaded, or the
// context deadline is hit. It returns a descriptive error on timeout so the
// caller can distinguish "page never loaded hCaptcha" from a solve failure.
func waitForHCaptchaReady(ctx context.Context, b browser) error {
	for {
		state, err := b.evaluate(ctx, hcaptchaReadyJS())
		if err == nil && strings.TrimSpace(state) == "ready" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("hcaptcha never finished loading on %s: %w", sunoCreateURL, ctx.Err())
		case <-time.After(hcaptchaLoadPollInterval):
		}
	}
}

func (s *solver) Solve(ctx context.Context, opts Options) (string, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	b, err := s.open(ctx, opts, false)
	if err != nil {
		return "", err
	}
	defer b.close()

	// Always seed the session from the user's logged-in Chrome cookies before
	// navigating: suno.com/create only loads the hCaptcha API for an
	// authenticated session, and the dedicated solver profile is not a reliable
	// persistence store — the Seeded flag can outlive the actual session, which
	// left the page logged out and hCaptcha unloaded.
	cookies, serr := s.seed(ctx)
	if serr != nil {
		return "", serr
	}
	if err := b.setCookies(ctx, cookies); err != nil {
		return "", err
	}
	if err := b.navigate(ctx); err != nil {
		return "", err
	}

	// suno.com/create loads the hCaptcha API asynchronously; calling
	// hcaptcha.render() before it finishes throws "hcaptcha is not defined".
	// Poll until the API is ready (matching paperfoot/suno-cli) before solving.
	if err := waitForHCaptchaReady(ctx, b); err != nil {
		return "", err
	}

	raw, err := b.evaluate(ctx, solveJS())
	if err != nil {
		return "", err
	}
	tok, interactiveNeeded, cerr := classifyToken(raw)
	if cerr != nil {
		return "", cerr
	}
	if tok != "" {
		return tok, nil
	}

	if !interactiveNeeded {
		return "", ErrInteractiveRequired
	}
	if !opts.Interactive {
		return "", ErrInteractiveRequired
	}

	if err := b.showOnScreen(ctx); err != nil {
		return "", err
	}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interactivePollInterval):
		}
		raw, err := b.evaluate(ctx, solveJS())
		if err != nil {
			return "", err
		}
		tok, _, cerr := classifyToken(raw)
		if cerr != nil {
			return "", cerr
		}
		if tok != "" {
			return tok, nil
		}
	}
}

// Stop tears down the managed Chrome for a single profile port.
func Stop(ctx context.Context, port int) error {
	return killManagedChrome(ctx, port)
}

// StatusFor reports whether a managed Chrome is running for the given profile.
func StatusFor(profile string, port int) ProfileStatus {
	return ProfileStatus{Profile: profile, Port: port, Running: portAlive(port)}
}

// loginOpen opens the visible profile window. It is a package var so login_test
// can substitute a fake browser without launching real Chrome.
var loginOpen = func(ctx context.Context, opts Options, visible bool) (browser, error) {
	return openBrowser(ctx, opts, visible)
}

// Login opens a visible window for the profile and navigates to suno.com so the
// user can establish/switch the account session, which then persists in the
// dedicated profile. It returns once the page has loaded; the window is left
// running so the user can sign in (and Solve reconnects to it via the CDP
// port). `auth captcha stop`, or closing the window, tears it down.
func Login(ctx context.Context, opts Options) error {
	b, err := loginOpen(ctx, opts, true)
	if err != nil {
		return err
	}
	if err := b.navigate(ctx); err != nil {
		// The window never reached Suno; tear it down rather than leave a
		// blank, useless Chrome running. (cmd.Context() is context.Background,
		// so a successfully-launched window survives process exit on its own —
		// closing it here on the happy path is the regression we are fixing.)
		b.close()
		return err
	}
	return nil
}
