# Computer Use via Command Line

The interesting product idea behind `macctx` is not screenshots. It is computer use expressed as shell primitives.

## The loop

```text
observe → decide → act → verify
```

- **Observe:** `macctx active`, `macctx see`, `macctx dump`
- **Decide:** agent/model reads JSON, screenshot path, UI map
- **Act:** `macctx act ... --execute` or lower-level Peekaboo
- **Verify:** run `macctx active` / `macctx see` again

## Why CLI instead of browser sidecar?

A CLI is composable:

- works in any agent runtime
- can be logged and replayed
- can be wrapped with approval gates
- can be run locally with no hosted control plane
- can expose dry-run plans before actions

## Safety boundary

Observation is usually safe. Action is risky. `macctx` keeps them separate.

`act` defaults to dry-run:

```bash
macctx-pp-cli act click --on B3
```

To execute:

```bash
macctx-pp-cli act click --on B3 --execute
```

In an agent runtime, require approval before adding `--execute`.

## Example: agent picks a button

```bash
macctx-pp-cli see --annotate --path /tmp/ui.png --json > /tmp/ui.json
```

Agent reads `/tmp/ui.json`, finds a button with id `B3`, then proposes:

```bash
macctx-pp-cli act click --on B3
```

Human approves:

```bash
macctx-pp-cli act click --on B3 --execute
```

## Example: browser search

```bash
macctx-pp-cli act focus --app Safari --execute
macctx-pp-cli act hotkey --keys cmd,l --execute
macctx-pp-cli act type "site:github.com agno agents" --return --execute
macctx-pp-cli see --app Safari --json --path /tmp/safari.png
```

## Example: create a support artifact

```bash
macctx-pp-cli dump --json --screenshot --see > /tmp/support-context.json
```

Attach the JSON and screenshot to a bug report.
