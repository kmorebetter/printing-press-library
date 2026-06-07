// Copyright 2026 horknfbr. Licensed under Apache-2.0. See LICENSE.
//
// Bridges the cobra/flags world to internal/captcha: resolves the active
// profile from --captcha-profile / SUNO_CAPTCHA_PROFILE / config, builds
// captcha.Options (dedicated user-data-dir + CDP port), and persists the
// `seeded` flag after a successful solve.

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/suno/internal/captcha"
	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/suno/internal/config"
)

// captchaProfileFlag is set by the gated generate commands.
var captchaProfileFlag string

// captchaProfilesDir is the parent of all dedicated solver profiles. Never the
// user's real Chrome dir.
func captchaProfilesDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "suno-pp-cli", "chrome-profiles")
}

// resolveCaptchaOptions loads config, resolves the profile (env > flag handled
// by passing env-or-flag), ensures it exists (assigning a port), and returns
// the Options plus a persist callback to mark the profile seeded.
func resolveCaptchaOptions(configPath string, interactive bool) (captcha.Options, func() error, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return captcha.Options{}, nil, err
	}
	sel := captchaProfileFlag
	if sel == "" {
		sel = os.Getenv("SUNO_CAPTCHA_PROFILE")
	}
	name := cfg.ResolveCaptchaProfile(sel)
	prof := cfg.EnsureCaptchaProfile(name)

	dir := prof.UserDataDir
	if dir == "" {
		dir = filepath.Join(captchaProfilesDir(), name)
	}
	opts := captcha.Options{
		Profile:     name,
		UserDataDir: dir,
		CDPPort:     prof.CDPPort,
		Seeded:      prof.Seeded,
		Interactive: interactive,
	}
	persistSeeded := func() error {
		prof.Seeded = true
		return cfg.SaveCaptcha()
	}
	if err := cfg.SaveCaptcha(); err != nil {
		return captcha.Options{}, nil, err
	}
	return opts, persistSeeded, nil
}

// defaultSolver is the production Solver, overridable in tests.
var defaultSolver captcha.Solver = captcha.New()

// solveCaptchaToken runs the solver for the active profile and returns a fresh
// token, persisting the seeded flag on success.
func solveCaptchaToken(ctx context.Context, configPath string, interactive bool) (string, error) {
	opts, persistSeeded, err := resolveCaptchaOptions(configPath, interactive)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(os.Stderr, "Solving hCaptcha in Chrome… if macOS asks to allow access to Chrome's data, approve it promptly (the solver waits up to "+captcha.DefaultTimeout.String()+").")
	tok, err := defaultSolver.Solve(ctx, opts)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("captcha solve timed out after %s — if macOS prompted to allow Chrome access, approve it and retry: %w", captcha.DefaultTimeout, err)
		}
		return "", err
	}
	_ = persistSeeded()
	return tok, nil
}
