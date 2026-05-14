// Copyright 2026 hiten-shah. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type actionPlan struct {
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	Description string   `json:"description"`
	DryRun      bool     `json:"dry_run"`
}

func newActCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "act",
		Short: "Propose or execute guarded Mac UI actions through Peekaboo",
		Long: strings.TrimSpace(`Propose or execute Mac UI actions through Peekaboo.

By default, act subcommands are dry-run: they print the exact Peekaboo command that would run.
Pass --execute to actually click, type, press keys, scroll, or focus windows.
This makes macctx safe for agent workflows: observe with macctx, decide in an agent, then require an explicit execution flag for the hands.`),
	}
	cmd.AddCommand(newActClickCmd())
	cmd.AddCommand(newActTypeCmd())
	cmd.AddCommand(newActHotkeyCmd())
	cmd.AddCommand(newActPressCmd())
	cmd.AddCommand(newActScrollCmd())
	cmd.AddCommand(newActFocusCmd())
	return cmd
}

func newActClickCmd() *cobra.Command {
	var on, coords, app, snapshot string
	var execute, asJSON bool
	cmd := &cobra.Command{
		Use:   "click",
		Short: "Click a UI element id or coordinate pair",
		Example: strings.TrimSpace(`macctx-pp-cli see --json --annotate
macctx-pp-cli act click --on B3
macctx-pp-cli act click --on B3 --execute
macctx-pp-cli act click --coords 640,420 --execute`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if on == "" && coords == "" {
				return fmt.Errorf("provide --on <element-id> or --coords x,y")
			}
			pb := peekabooArgs("click")
			pb = addFlag(pb, "--on", on)
			pb = addFlag(pb, "--coords", coords)
			pb = addFlag(pb, "--app", app)
			pb = addFlag(pb, "--snapshot", snapshot)
			return runOrDescribeAction(actionPlan{Command: "peekaboo", Args: pb, Description: "Click a UI target", DryRun: !execute}, execute, asJSON)
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "Peekaboo element id from macctx see, e.g. B3")
	cmd.Flags().StringVar(&coords, "coords", "", "coordinate pair x,y")
	cmd.Flags().StringVar(&app, "app", "", "target app name")
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Peekaboo snapshot id")
	cmd.Flags().BoolVar(&execute, "execute", false, "actually execute the action; default is dry-run")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "emit JSON plan/result")
	return cmd
}

func newActTypeCmd() *cobra.Command {
	var app string
	var execute, asJSON, ret bool
	cmd := &cobra.Command{
		Use:   "type <text>",
		Short: "Type text into the active or targeted app",
		Example: strings.TrimSpace(`macctx-pp-cli act type "hello world"
macctx-pp-cli act type "hello world" --execute
macctx-pp-cli act type "search query" --app Safari --return --execute`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := strings.Join(args, " ")
			pb := peekabooArgs("type", text)
			pb = addFlag(pb, "--app", app)
			pb = addBoolFlag(pb, "--return", ret)
			return runOrDescribeAction(actionPlan{Command: "peekaboo", Args: pb, Description: "Type text", DryRun: !execute}, execute, asJSON)
		},
	}
	cmd.Flags().StringVar(&app, "app", "", "target app name")
	cmd.Flags().BoolVar(&ret, "return", false, "press Return after typing")
	cmd.Flags().BoolVar(&execute, "execute", false, "actually execute the action; default is dry-run")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "emit JSON plan/result")
	return cmd
}

func newActHotkeyCmd() *cobra.Command {
	var app, keys string
	var execute, asJSON bool
	cmd := &cobra.Command{
		Use:   "hotkey --keys cmd,s",
		Short: "Press a keyboard shortcut",
		Example: strings.TrimSpace(`macctx-pp-cli act hotkey --keys cmd,s
macctx-pp-cli act hotkey --keys cmd,shift,t --execute`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if keys == "" {
				return fmt.Errorf("provide --keys, e.g. cmd,s")
			}
			pb := peekabooArgs("hotkey", "--keys", keys)
			pb = addFlag(pb, "--app", app)
			return runOrDescribeAction(actionPlan{Command: "peekaboo", Args: pb, Description: "Press hotkey", DryRun: !execute}, execute, asJSON)
		},
	}
	cmd.Flags().StringVar(&keys, "keys", "", "comma-separated keys, e.g. cmd,s or cmd,shift,t")
	cmd.Flags().StringVar(&app, "app", "", "target app name")
	cmd.Flags().BoolVar(&execute, "execute", false, "actually execute the action; default is dry-run")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "emit JSON plan/result")
	return cmd
}

func newActPressCmd() *cobra.Command {
	var app, key string
	var count int
	var execute, asJSON bool
	cmd := &cobra.Command{
		Use:   "press --key tab",
		Short: "Press a special key one or more times",
		RunE: func(cmd *cobra.Command, args []string) error {
			if key == "" {
				return fmt.Errorf("provide --key, e.g. tab, return, escape")
			}
			pb := peekabooArgs("press", key)
			if count > 1 {
				pb = append(pb, "--count", strconv.Itoa(count))
			}
			pb = addFlag(pb, "--app", app)
			return runOrDescribeAction(actionPlan{Command: "peekaboo", Args: pb, Description: "Press key", DryRun: !execute}, execute, asJSON)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "key to press, e.g. tab, return, escape")
	cmd.Flags().IntVar(&count, "count", 1, "number of key presses")
	cmd.Flags().StringVar(&app, "app", "", "target app name")
	cmd.Flags().BoolVar(&execute, "execute", false, "actually execute the action; default is dry-run")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "emit JSON plan/result")
	return cmd
}

func newActScrollCmd() *cobra.Command {
	var direction, amount, app string
	var execute, asJSON, smooth bool
	cmd := &cobra.Command{
		Use:   "scroll --direction down",
		Short: "Scroll the active or targeted app/window",
		RunE: func(cmd *cobra.Command, args []string) error {
			if direction == "" {
				direction = "down"
			}
			pb := peekabooArgs("scroll", "--direction", direction)
			pb = addFlag(pb, "--amount", amount)
			pb = addFlag(pb, "--app", app)
			pb = addBoolFlag(pb, "--smooth", smooth)
			return runOrDescribeAction(actionPlan{Command: "peekaboo", Args: pb, Description: "Scroll", DryRun: !execute}, execute, asJSON)
		},
	}
	cmd.Flags().StringVar(&direction, "direction", "down", "scroll direction: up, down, left, right")
	cmd.Flags().StringVar(&amount, "amount", "", "scroll amount/ticks")
	cmd.Flags().StringVar(&app, "app", "", "target app name")
	cmd.Flags().BoolVar(&smooth, "smooth", false, "use smooth scrolling")
	cmd.Flags().BoolVar(&execute, "execute", false, "actually execute the action; default is dry-run")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "emit JSON plan/result")
	return cmd
}

func newActFocusCmd() *cobra.Command {
	var app, windowTitle string
	var execute, asJSON bool
	cmd := &cobra.Command{
		Use:   "focus --app Safari",
		Short: "Focus an app or window",
		RunE: func(cmd *cobra.Command, args []string) error {
			if app == "" {
				return fmt.Errorf("provide --app <name>")
			}
			pb := peekabooArgs("window", "focus", "--app", app)
			pb = addFlag(pb, "--window-title", windowTitle)
			return runOrDescribeAction(actionPlan{Command: "peekaboo", Args: pb, Description: "Focus app/window", DryRun: !execute}, execute, asJSON)
		},
	}
	cmd.Flags().StringVar(&app, "app", "", "target app name")
	cmd.Flags().StringVar(&windowTitle, "window-title", "", "target window title")
	cmd.Flags().BoolVar(&execute, "execute", false, "actually execute the action; default is dry-run")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "emit JSON plan/result")
	return cmd
}

func runOrDescribeAction(plan actionPlan, execute bool, asJSON bool) error {
	if !execute {
		if asJSON {
			return printJSON(plan)
		}
		fmt.Println("Dry run. Add --execute to run:")
		fmt.Println(shellQuoteCommand(append([]string{plan.Command}, plan.Args...)))
		return nil
	}
	if err := mustHavePeekaboo(); err != nil {
		return err
	}
	res, err := runPeekaboo(plan.Args...)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(map[string]any{
			"executed": true,
			"command":  plan.Command,
			"args":     plan.Args,
			"stdout":   strings.TrimSpace(res.Stdout),
			"stderr":   strings.TrimSpace(res.Stderr),
		})
	}
	return writePeekabooOutput(res, false)
}

func shellQuoteCommand(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_=/:.,", r))
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
