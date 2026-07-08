---
name: pp-janeapp
description: "Book, view, and manage your Jane (janeapp.com) appointments across every clinic from one terminal — with a unified agenda, next-opening finder, and availability watch the patient portal can't do. Trigger phrases: `book a physio appointment`, `when is my next appointment`, `check my jane appointments`, `find the earliest opening`, `reschedule my appointment`, `use janeapp`, `run janeapp`."
author: "Omar Shahine"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - janeapp-pp-cli
    install:
      - kind: go
        bins: [janeapp-pp-cli]
        module: github.com/mvanhorn/printing-press-library/library/health/janeapp/cmd/janeapp-pp-cli
---

# Jane App — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `janeapp-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer. It defaults binaries to `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows:
   ```bash
   npx -y @mvanhorn/printing-press-library install janeapp --cli-only
   ```
2. Verify: `janeapp-pp-cli --version`
3. Ensure the reported install directory is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails (no Node, offline, etc.), fall back to a direct Go install (requires Go 1.26.5 or newer). This installs into `$GOPATH/bin` (default `$HOME/go/bin`), so add that directory to `$PATH` instead:

```bash
go install github.com/mvanhorn/printing-press-library/library/health/janeapp/cmd/janeapp-pp-cli@latest
```

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

Jane is the booking platform behind thousands of physio, massage, chiro, and wellness clinics, but each clinic is a separate subdomain with its own login and no public API. This CLI holds a profile per clinic, imports the session from a browser where you're already logged in (Jane gates password login behind reCAPTCHA), and unifies booking, viewing, and managing appointments across all of them. `agenda` merges every booking into one view; `next-opening` pages past Jane's 7-day availability cap to find the soonest slot.

## When to Use This CLI

Use this CLI when you are a patient at one or more Jane clinics and want to book, check, or reschedule appointments without logging into each clinic's web portal separately. It shines when you juggle multiple providers on Jane and want a single agenda and availability search across all of them.

## When Not to Use This CLI

Do not activate this CLI for requests that require creating, updating, deleting, publishing, commenting, upvoting, inviting, ordering, sending messages, booking, purchasing, or changing remote state. This printed CLI exposes read-only commands for inspection, export, sync, and analysis.

## Unique Capabilities

These capabilities aren't available in any other tool for this API.

### Cross-clinic
- **`agenda`** — See every appointment across every Jane clinic you use in one chronological view.

  _One call answers 'what do I have coming up anywhere' instead of logging into each clinic portal separately._

  ```bash
  janeapp-pp-cli agenda --agent
  ```
- **`conflict-check`** — Before booking, warn if a candidate slot collides with an existing appointment at another clinic.

  _Prevents double-booking yourself across different clinics._

  ```bash
  janeapp-pp-cli conflict-check --at 2026-07-15T09:00:00 --duration 60
  ```

### Availability intelligence
- **`next-opening`** — Find the soonest available slot for a practitioner + treatment, paging past Jane's 7-day availability cap.

  _Answers 'when is the earliest I can get in' without clicking week-by-week through the portal._

  ```bash
  janeapp-pp-cli next-opening --clinic embophysio --treatment 1 --staff 1
  ```
- **`watch`** — Poll availability and alert when an earlier slot than a target opens up.

  _Catches cancellations that free up an earlier appointment._

  ```bash
  janeapp-pp-cli watch --clinic embophysio --treatment 1 --staff 1 --before 2026-08-01
  ```

## Command Reference

**appointments** — Your own appointments at the clinic (requires a logged-in session).

- `janeapp-pp-cli appointments upcoming` / `janeapp-pp-cli appointments past` — View your upcoming or past appointments (add `--all-clinics` to merge every logged-in clinic). Requires a logged-in session.

**disciplines** — Disciplines (categories of care) offered by the clinic, e.g. Physical Therapy.

- `janeapp-pp-cli disciplines` — List disciplines (service categories) with descriptions.

**locations** — Clinic locations (address, phone, booking URL) for the active profile's Jane instance.

- `janeapp-pp-cli locations` — List clinic locations with address, contact info, and booking URL.

**openings** — Live availability (openings) for a practitioner + treatment at a location.

- `janeapp-pp-cli openings` — List available appointment openings for a staff member + treatment at a location over a date window.

**staff** — Practitioners (staff members) and the treatments they offer.

- `janeapp-pp-cli staff` — List practitioners, their bookable treatment IDs, and online-booking availability.

**treatments** — Bookable treatments/services with price, duration, and online-booking eligibility.

- `janeapp-pp-cli treatments` — List treatments (services) with price, duration, discipline, and whether they can be booked online.


## Freshness Contract

This printed CLI owns bounded freshness only for registered store-backed read command paths. In `--data-source auto` mode, those paths check `sync_state` and may run a bounded refresh before reading local data. `--data-source local` never refreshes. `--data-source live` reads the API and does not mutate the local store. Set `JANEAPP_NO_AUTO_REFRESH=1` to skip the freshness hook without changing source selection.

Covered paths:

- `janeapp-pp-cli disciplines`
- `janeapp-pp-cli disciplines get`
- `janeapp-pp-cli disciplines list`
- `janeapp-pp-cli disciplines search`
- `janeapp-pp-cli locations`
- `janeapp-pp-cli locations get`
- `janeapp-pp-cli locations list`
- `janeapp-pp-cli locations search`
- `janeapp-pp-cli openings`
- `janeapp-pp-cli openings get`
- `janeapp-pp-cli openings list`
- `janeapp-pp-cli openings search`
- `janeapp-pp-cli staff`
- `janeapp-pp-cli staff get`
- `janeapp-pp-cli staff list`
- `janeapp-pp-cli staff search`
- `janeapp-pp-cli treatments`
- `janeapp-pp-cli treatments get`
- `janeapp-pp-cli treatments list`
- `janeapp-pp-cli treatments search`

When JSON output uses the generated provenance envelope, freshness metadata appears at `meta.freshness`. Treat it as current-cache freshness for the covered command path, not a guarantee of complete historical backfill or API-specific enrichment.

### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
janeapp-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Recipes

### Unified agenda across clinics

```bash
janeapp-pp-cli agenda --agent --select clinic,date,start_at,practitioner,treatment
```

Every upcoming appointment from every logged-in clinic, narrowed to the fields an agent needs.

### Earliest slot with a specific PT

```bash
janeapp-pp-cli next-opening --clinic embophysio --treatment 1 --staff 1
```

Stitches 7-day windows until it finds the first available opening.

### Export appointments to a calendar file

```bash
janeapp-pp-cli calendar --all-clinics --out ~/jane.ics
```

Generates an ICS from your appointments across every clinic; import into Apple/Google Calendar or subscribe live with 'calendar --url'.

### Book a slot (dry-run first)

```bash
janeapp-pp-cli book --clinic embophysio --treatment 1 --staff 1 --location 1 --at 2026-07-15T09:00:00
```

Shows the reserve/confirm request without writing; add --confirm to actually book.

## Auth Setup

Each Jane clinic is its own subdomain with a separate patient account, and Jane gates username/password login behind reCAPTCHA. So the CLI imports the _front_desk_session cookie from a browser where you're already logged in: register a clinic (`clinic add <name> --url=https://<clinic>.janeapp.com`), log in to it once in your browser, then run `auth login --clinic <name> --chrome` (or `--cookies-file <file>`). Repeat per clinic; read commands accept --all-clinics.

Run `janeapp-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  janeapp-pp-cli appointments --agent --select id,name,status
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Read-only** — do not use this CLI for create, update, delete, publish, comment, upvote, invite, order, send, or other mutating requests

### Response envelope

Commands that read from the local store or the API wrap output in a provenance envelope:

```json
{
  "meta": {"source": "live" | "local", "synced_at": "...", "reason": "..."},
  "results": <data>
}
```

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal AND no machine-format flag (`--json`, `--csv`, `--compact`, `--quiet`, `--plain`, `--select`) is set — piped/agent consumers and explicit-format runs get pure JSON on stdout.

## Paths and state

Agents should treat the CLI's path resolver as part of the runtime contract:

- Use `--home <dir>` for one invocation, or set `JANEAPP_HOME=<dir>` to relocate all four path kinds under one root.
- Use per-kind env vars only when a specific kind must diverge: `JANEAPP_CONFIG_DIR`, `JANEAPP_DATA_DIR`, `JANEAPP_STATE_DIR`, `JANEAPP_CACHE_DIR`.
- Resolution order is per-kind env var, `--home`, `JANEAPP_HOME`, XDG (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`), then platform defaults.
- `config` contains settings like `config.toml` and profiles. `data` contains `credentials.toml`, `data.db`, cookies, and auth sidecars. `state` contains persisted queries, jobs, and `teach.log`. `cache` contains regenerable HTTP/cache files.
- Stored secrets live in `credentials.toml` under the data dir. Existing legacy `config.toml` secrets are read for compatibility and leave `config.toml` on the first auth write.
- Run `janeapp-pp-cli doctor --fail-on warn` to surface path and credential-location warnings. `agent-context` exposes a schema v4 `paths` block for agents that need the resolved dirs.
- For MCP, pass relocation through the MCP host config. The MCP binary does not inherit CLI flags:

  ```json
  {
    "mcpServers": {
      "janeapp": {
        "command": "janeapp-pp-mcp",
        "env": {
          "JANEAPP_HOME": "/srv/janeapp"
        }
      }
    }
  }
  ```

Fleet precedence: an inherited per-kind env var overrides an explicit `--home` for that kind. Use `JANEAPP_HOME` or per-kind vars as durable fleet levers, and use `--home` only for a single invocation. Relocation is not reversible by unsetting env vars; move files manually before clearing `JANEAPP_HOME`, or `doctor` will not find credentials left under the former root.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
janeapp-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
janeapp-pp-cli feedback --stdin < notes.txt
janeapp-pp-cli feedback list --json --limit 10
```

Entries are stored locally as `feedback.jsonl` under the resolved data dir. They are never POSTed unless `JANEAPP_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `JANEAPP_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

Write what *surprised* you, not a bug report. Short, specific, one line: that is the part that compounds.

## Output Delivery

Every command accepts `--deliver <sink>`. The output goes to the named sink in addition to (or instead of) stdout, so agents can route command results without hand-piping. Three sinks are supported:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` (tmp + rename) |
| `webhook:<url>` | POST the output body to the URL (`application/json` or `application/x-ndjson` when `--compact`) |

Unknown schemes are refused with a structured error naming the supported set. Webhook failures return non-zero and log the URL + HTTP status on stderr.

## Named Profiles

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled agent calls the same command every run with the same configuration - HeyGen's "Beacon" pattern.

```
janeapp-pp-cli profile save briefing --json
janeapp-pp-cli --profile briefing appointments
janeapp-pp-cli profile list --json
janeapp-pp-cli profile show briefing
janeapp-pp-cli profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (wrong arguments) |
| 3 | Resource not found |
| 4 | Authentication required |
| 5 | API error (upstream issue) |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `janeapp-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

1. Install the MCP server:
   ```bash
   go install github.com/mvanhorn/printing-press-library/library/health/janeapp/cmd/janeapp-pp-mcp@latest
   ```
2. Register with Claude Code:
   ```bash
   claude mcp add janeapp-pp-mcp -- janeapp-pp-mcp
   ```
3. Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which janeapp-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   janeapp-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `janeapp-pp-cli <command> --help`.
