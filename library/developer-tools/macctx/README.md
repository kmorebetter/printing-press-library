# macctx

Give agents eyes, short-term memory, and guarded hands on your Mac.

`macctx` is an agent-native macOS context CLI built on top of [Peekaboo](https://peekaboo.boo). Peekaboo is the low-level UI automation engine. `macctx` is the safer, higher-level shell surface for agent workflows: observe the Mac, produce structured context, and propose or execute UI actions.

## Quick Start

```bash
# Verify Peekaboo + permissions
macctx-pp-cli doctor

# See what is active
macctx-pp-cli active
macctx-pp-cli active --json

# Capture context for an agent
macctx-pp-cli dump --json --screenshot --see
macctx-pp-cli handoff --clipboard

# Inspect UI targets
macctx-pp-cli see --annotate --path /tmp/ui.png --json

# Propose a UI action without executing it
macctx-pp-cli act click --on B3

# Execute only when explicit
macctx-pp-cli act click --on B3 --execute
```

## Mental Model

Computer use from the command line is a loop:

```text
observe → decide → act
```

`macctx` handles the observation and handoff layer:

```bash
macctx-pp-cli active
macctx-pp-cli windows
macctx-pp-cli screenshot --window
macctx-pp-cli see --json
macctx-pp-cli dump --json --screenshot --see
```

An agent or script decides what to do with that context.

`macctx act` provides guarded hands. It is dry-run by default and requires `--execute` to perform UI actions:

```bash
macctx-pp-cli act click --on B3              # dry run
macctx-pp-cli act click --on B3 --execute    # real click
```

## Commands

### Observation

```bash
macctx-pp-cli active [--json]
macctx-pp-cli apps [--json]
macctx-pp-cli windows [--app Safari] [--json]
macctx-pp-cli screenshot [--window|--screen] [--path file.png] [--json]
macctx-pp-cli see [--annotate] [--path ui.png] [--json]
macctx-pp-cli clipboard [--json] [--full] [--limit 500]
```

### Agent context

```bash
macctx-pp-cli dump --json --screenshot --see --clipboard
macctx-pp-cli handoff --clipboard
```

### Guarded computer use

```bash
macctx-pp-cli act click --on B3
macctx-pp-cli act click --coords 640,420
macctx-pp-cli act type "hello world"
macctx-pp-cli act hotkey --keys cmd,s
macctx-pp-cli act press --key tab --count 2
macctx-pp-cli act scroll --direction down --amount 5
macctx-pp-cli act focus --app Safari
```

All `act` commands are dry-run by default. Add `--execute` to perform the action.

## Creative Use Cases

### 1. Agent handoff

```bash
macctx-pp-cli handoff --clipboard
```

Creates a concise markdown summary of the active app/window and optional privacy-safe clipboard preview. Paste it into any coding agent.

### 2. Bug report pack

```bash
macctx-pp-cli dump --json --screenshot --see > bug-context.json
```

Captures active app, window metadata, screenshot path, and UI element map for reproducible bug reports.

### 3. UI target selection

```bash
macctx-pp-cli see --annotate --path /tmp/ui.png --json > ui.json
```

The agent reads `ui.json`, chooses a Peekaboo element id, then proposes:

```bash
macctx-pp-cli act click --on B12
```

A human or approval system can inspect before adding `--execute`.

### 4. Safe browser/app automation

```bash
macctx-pp-cli act focus --app Safari --execute
macctx-pp-cli see --app Safari --annotate --json
macctx-pp-cli act click --on T4 --execute
macctx-pp-cli act type "search term" --return --execute
```

This is browser/computer use without a resident browser sidecar: the shell is the control plane.

### 5. Session resume

```bash
macctx-pp-cli handoff > ~/Desktop/current-mac-context.md
```

Useful after interruptions: “what was I looking at, what app was active, what context should the next agent know?”

## Privacy + Safety Defaults

- Local-first: Peekaboo is called with `--no-remote`.
- Clipboard is preview-only by default.
- Full clipboard text requires `--full`.
- UI actions are dry-run by default.
- Real UI actions require explicit `--execute`.
- Destructive actions should still be routed through human approval in your agent runtime.

## Requirements

- macOS
- Peekaboo installed and available as `peekaboo`
- Screen Recording permission
- Accessibility permission for UI actions

Check readiness:

```bash
macctx-pp-cli doctor --json
```

## Agent Usage

A coding agent can use this pattern:

```bash
CTX=$(macctx-pp-cli dump --json --screenshot --see)
# Agent reads CTX, chooses action.
macctx-pp-cli act click --on B3          # proposal
macctx-pp-cli act click --on B3 --execute # after approval
```

## Health Check

```bash
macctx-pp-cli doctor
macctx-pp-cli active --json
macctx-pp-cli see --json --path /tmp/macctx-see.png
```

## Troubleshooting

- `peekaboo not found`: install Peekaboo or add it to `PATH`.
- Permission errors: enable Screen Recording and Accessibility in System Settings → Privacy & Security.
- Empty clipboard errors: clipboard may actually be empty; use `clipboard --json` for structured result handling.
- No clickable target: run `see --annotate --path /tmp/ui.png` and choose an element id from the UI map.

## Cookbook

### Create an agent prompt from your Mac

```bash
macctx-pp-cli handoff --clipboard > /tmp/handoff.md
```

### Capture UI state before asking for help

```bash
macctx-pp-cli dump --json --screenshot --see > /tmp/macctx.json
```

### Propose a save action

```bash
macctx-pp-cli act hotkey --keys cmd,s
```

### Execute the save action

```bash
macctx-pp-cli act hotkey --keys cmd,s --execute
```

## Unique Features

These capabilities aren't available in any other tool for this API.

### Agent context
- **`dump`** — Produce an agent-friendly Mac context bundle with active app, windows, optional screenshot, UI inspection, and clipboard preview.

  _Agents need observability before they can safely help with desktop workflows._

  ```bash
  macctx-pp-cli dump --json --screenshot --see --clipboard
  ```
- **`handoff`** — Generate a concise markdown handoff describing the visible and active Mac context for pasting into an AI agent.

  _Makes the user's current Mac state portable across agent sessions._

  ```bash
  macctx-pp-cli handoff --clipboard
  ```

### Guarded actions
- **`act`** — Propose or execute Peekaboo-backed UI actions with dry-run defaults and explicit --execute for real clicks, typing, keys, scrolling, and focus.

  _Computer use becomes auditable shell commands instead of opaque remote control._

  ```bash
  macctx-pp-cli act click --on B3
  ```

### Safety
- **`clipboard`** — Inspect clipboard type and preview text safely by default, requiring --full before printing full clipboard contents.

  _Keeps agent context useful without silently exfiltrating secrets._

  ```bash
  macctx-pp-cli clipboard --json
  ```
