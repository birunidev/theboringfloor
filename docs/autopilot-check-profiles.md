# Autopilot check profiles

Before this change `BoardConfig.Checks` ran for every card on a board. A
frontend-only slice was judged by Python checks that could pass on nothing
(`pytest` exit 5, "no tests collected", was allowed), so the evidence could be
green without verifying the change.

A **profile** is a named, explicit set of checks chosen per card. Checks are
never taken from card text and never chosen by a model.

## Selection

- `BoardConfig.Profiles` is a list. A profile is matched by `title_prefixes`
  (case-insensitive, trimmed), the same rule lanes use.
- No match: `ErrNoProfile`. More than one match: `ErrAmbiguousProfile`.
- The refusal happens before any board write: no claim comment, no move, no
  worktree. `Tick` skips the card with a log line, `TickParallel` fails the
  whole batch (like an unmatched lane), and a direct `RunCard` returns the
  error. The dry run reports it as the card's error.
- A board with no profiles keeps the old board-wide `checks`. A board with
  profiles must not also set `checks`.

## Validation (config load, fail closed)

`Config.Validate` refuses:

- duplicate or invalid profile names, empty `title_prefixes`, a profile with no
  checks, and overlapping prefixes between profiles;
- forbidden commands: shells, `sudo`/`su`/`env`/`xargs`, `curl`/`wget`/`ssh`,
  `rm`/`dd`/`chmod`, `npx`;
- inline code (`python -c`, `node -e`/`--eval`) and `npm|yarn|pnpm exec|x|dlx`;
- `allow_exit` containing 5 on a pytest command, on profile checks **and** on
  the legacy board-wide checks;
- `network: true` on anything except `npm ci --ignore-scripts`;
- `require_files` / `require_npm.dir` that are absolute or escape the worktree.

## Runtime (every check, every round)

- **Preconditions** (`require_files`, `require_npm`) run first. A missing
  lockfile, a missing `package.json` script, or a script that does no real work
  (`echo`, `true`, `exit 0`, `|| true`) is a *failed check*, so the agent can
  fix it and the run can never pass on nothing.
- pytest exit 5 is a failure even if a config allows it.
- Output saying nothing ran is a failure even at exit 0: `collected 0 items`,
  `no tests ran`, `No test files found`, `# tests 0`, `No tests found`, and a
  summary of only skipped tests (for example no Docker means every PostgreSQL
  test is skipped).
- A missing executable is a failed check (exit -1).
- Scope, sensitive-file and empty-diff gates (`pipeline.CodeGates`) are
  unchanged and still apply to every profile.

## Environment and sandbox

- Checks and setup commands now start from a minimal environment (`PATH`,
  `HOME`, `LANG`, `CI`). Before, setup ran unsandboxed with the member's full
  environment.
- Checks run in bubblewrap: read-only root, empty `$HOME`, writable worktree,
  no network, cleared environment.
- One exception: a check with `network: true` shares the host network. It must
  be `npm ci --ignore-scripts`, the registry is pinned through
  `npm_config_registry`, lifecycle scripts are off, the npm cache is a private
  tmpfs, and `$HOME` is still empty (no `~/.npmrc`). bubblewrap cannot filter
  hosts, so the pin is advisory at the network level; a hostile lockfile can
  still name other URLs. Review lockfile diffs for new `resolved` hosts.
- `node_modules/` is written inside the worktree. The Core repo ignores it, so
  it does not appear as an out-of-scope untracked file.
- Node under nvm is reached by listing the version directory in
  `sandbox_read_only`.

## Known limits

- The Working-list move still happens at claim, before the agent pre-flight.
  This change guarantees profile, STOP and budget admission happen first; it
  does not defer the move until the agent says the card is ready.
- `npm ci` needs a committed lockfile. A card that creates the lockfile
  (BII-898) passes the install check only after the agent writes it.
- A static profile cannot name a per-card test file. A backend profile must
  point at a stable runnable test target.

## Config migration proposal (redacted; not applied)

The private config is not edited by this change. After the new binary is built,
`allow_exit: [5]` makes the current config fail validation on purpose, so
migrate first. Paths are placeholders.

```json
{
  "board_id": "board_938353e08e72",
  "name": "V2 Engineering",
  "checks": [],
  "sandbox_read_only": ["<uv python dir>", "<venv dir>"],
  "profiles": [
    {
      "name": "core-frontend",
      "title_prefixes": ["[V2 FE"],
      "sandbox_read_only": ["<home>/.nvm/versions/node/<version>"],
      "require_files": ["frontend/package-lock.json"],
      "require_npm": {
        "dir": "frontend",
        "scripts": ["test", "typecheck", "build"]
      },
      "checks": [
        {"name": "install", "argv": ["npm", "--prefix", "frontend", "ci", "--ignore-scripts"], "network": true},
        {"name": "test", "argv": ["npm", "--prefix", "frontend", "run", "test"]},
        {"name": "lint", "argv": ["frontend/node_modules/.bin/oxlint", "frontend/src"]},
        {"name": "typecheck", "argv": ["npm", "--prefix", "frontend", "run", "typecheck"]},
        {"name": "build", "argv": ["npm", "--prefix", "frontend", "run", "build"]}
      ]
    },
    {
      "name": "core-backend",
      "title_prefixes": ["[V2 BE", "[V2 API"],
      "checks": [
        {"name": "compile", "argv": ["<venv>/bin/python", "-m", "compileall", "-q", "backend/app", "backend/tests"]},
        {"name": "core slice tests", "argv": ["<venv>/bin/python", "-m", "pytest", "-q", "-p", "no:cacheprovider", "backend/tests/test_core_slice_local.py"]}
      ]
    }
  ]
}
```

Notes:

- At the pinned Core base the frontend has no `test` or `typecheck` script, so
  `core-frontend` fails its preconditions until the card adds them. That is the
  intended behavior for BII-898, which owns that toolchain.
- Measured on Fedora at the pinned base `d79518b3` after `npm ci`:
  `npm run build` exits 2 with 192 TypeScript errors (98 are `TS2307`, a
  missing module such as `../components/ui`, `@/components/shadcn/ui/*` or
  `./routes/Landing`; 88 are `TS7006` implicit `any` that follow from them).
  `tsc -b` alone also exits 2. The profile reports this and does not waive it,
  so every frontend card ends Blocked until the clean-room import closure
  lands. Whether a toolchain-only slice such as BII-898 may use a narrower
  profile without `build`/`typecheck` is a CTO decision, not made here; if
  taken, give that profile its own non-overlapping title prefixes.
- `npm run lint` runs `oxlint` with no ignore, so after `npm ci` it scans
  `node_modules` and exits 1 (one error under `node_modules/@tailwindcss`)
  even though `frontend/src` only has a warning. The proposal therefore calls
  the binary directly on `frontend/src` (exit 0 at the base). Add
  `frontend/tests` once it exists.
- `core-backend` needs Docker and PostgreSQL. Without them every test is
  skipped, which the runner now counts as a failure.
- No `core-docs` or Lab profiles are proposed: no verified documentation check
  exists, and Lab titles need an explicit prefix decision before a profile can
  be enabled.
