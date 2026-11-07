# Frontend/backend parallel lanes

Parallel mode is explicit:

```sh
./bin/autopilot --live --parallel --board "V2 Engineering"
./bin/autopilot --live --parallel --once --board "V2 Engineering"
```

The process still takes the one global autopilot lock. Inside that process it
may run at most one card in each configured lane concurrently. The workers use
different per-card worktrees and branches. They share the existing STOP switch,
daily and per-card token ledger, permission policy, sandbox requirement and
draft-PR-only publisher. A global STOP cancels every lane. No lane can route to
Done.

Git fetch/worktree preparation is serialized when lanes share a repository, to
avoid object-store and `FETCH_HEAD` lock races. Model work and sandboxed checks
run concurrently after those distinct worktrees exist. Board API calls are also
serialized around the shared MCP session and OAuth refresh source. Immediately
before claiming, each card is re-read and must still be in Autopilot Queue.

V2 Core Engineering currently defines `Frontend In Progress` and
`Backend In Progress`, followed by `Integration & QA` and external `Review`.
Configure the two coding lanes on its existing board mapping:

```json
"lanes": [
  {
    "name": "frontend",
    "title_prefixes": ["[V2 FE", "[V2 OSS CORE FE"],
    "working_list": "Frontend In Progress"
  },
  {
    "name": "backend",
    "title_prefixes": ["[V2 BE", "[V2 OSS CORE BE", "[V2 API"],
    "working_list": "Backend In Progress"
  }
]
```

Prefixes are case-insensitive and anchored at the start of the title. Empty,
overlapping or duplicate lane definitions are rejected. Before any board write,
the scheduler resolves every queued card in the batch. A card that matches no
lane or more than one lane aborts the whole batch. It never uses free-form card
description text to guess frontend/backend ownership.

The normal `--live` loop stays single-card and ignores lanes. Use `--parallel`
only after the board mapping has been reviewed. Cards already in either working
list are not eligible because selection still reads only `Autopilot Queue`.
Existing card/board founder holds remain pre-flight inputs. A title match is
routing, not authorization.

Parallel state is written atomically to
`~/.theboringfloor/autopilot/workers.json`:

```json
{
  "version": 1,
  "updatedAt": "2026-11-07T00:00:00Z",
  "workers": [
    {"lane": "birunidev-space/backend", "state": "running", "card": "BII-...", "stage": "preflight"},
    {"lane": "birunidev-space/frontend", "state": "running", "card": "BII-...", "stage": "implement:1"}
  ]
}
```

Entries are sorted by lane. While two lanes are active, legacy `status.json`
reports `state=running` and points monitors to `workers.json`; with one lane it
mirrors that lane, and after both finish it returns to idle. Run logs remain
separate by card/run.

Board context checked on 7 November 2026: the Autopilot Queue and Frontend In
Progress lists were empty. Backend In Progress contained BII-504, whose card
explicitly says `ACTIVE_OWNER` and `NOT QUEUEABLE`; parallel mode does not select
it and must not duplicate its branch.
