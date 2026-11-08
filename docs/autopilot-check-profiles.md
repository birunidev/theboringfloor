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
- any `allow_exit`, on profile checks, legacy board-wide checks and setup. Every
  check is a required gate and passes only on exit 0; accepting pytest 5,
  vitest 1 or tsc 2 would let a gate pass on failure. At runtime `allow_exit`
  is ignored too, so a stale config cannot weaken a gate;
- `network: true` on anything but the exact argv `npm ci --ignore-scripts` or
  `npm --prefix <relative dir> ci --ignore-scripts`. Any other flag is refused
  (`--registry`, `--userconfig`, `--ignore-scripts=false`, `--global`,
  `--cache`, an absolute or `..` prefix, `--prefix=x`), and so is any `network`
  on a setup command;
- `setup` commands that are forbidden commands (they are validated like checks);
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
- Paths in `require_files` and `require_npm.dir` are resolved through symlinks
  and must stay inside the real worktree. A `frontend -> /etc` link, or a
  `package.json` that points outside, is refused.
- `RunCard` itself checks STOP and both budgets at the claim boundary, so a
  direct call cannot bypass what `Tick` and `TickParallel` already check.
- Scope, sensitive-file and empty-diff gates (`pipeline.CodeGates`) are
  unchanged and still apply to every profile.

## Environment and sandbox

- **Setup runs in the sandbox.** It used to run on the host with the member's
  environment. Now setup commands run in the same bubblewrap sandbox as the
  checks: read-only root, empty `$HOME`, writable worktree, no network, cleared
  environment. A setup command has no more access to the member's files than
  the code it prepares. `ln -s <repo>/vendor vendor` still works because the
  link is created in the worktree.
- Checks start from a minimal environment (`PATH`, `HOME`, `LANG`, `CI`). `HOME`
  is the worktree outside the sandbox, never the member's real home. This is
  environment hygiene, not a filesystem boundary; the sandbox is the boundary.
- One exception to "no network": a check with `network: true` shares the host
  network. Before it runs, `PreflightNetworkInstall` refuses it unless:
  - the argv is the exact allowlisted install (above);
  - no `.npmrc`, `.yarnrc`, `.yarnrc.yml` or `.pnpmrc` exists in the worktree
    root or the project directory (they could redirect a scoped registry or
    re-enable scripts, which an environment variable cannot override);
  - the project directory and `package-lock.json` resolve inside the worktree;
  - the lockfile is version 2 or 3, has no `link` entries, and every package
    resolves from `https://registry.npmjs.org/` and carries a sha512/sha384
    integrity hash.
  The sandbox also sets `npm_config_registry`, `ignore_scripts`, a private
  cache, and `userconfig`/`globalconfig` to `/dev/null`, with `$HOME` empty.
- **This is not an egress firewall.** bubblewrap cannot filter hosts, so the
  network is open for the duration of that one command. The protection is on
  what npm is told to fetch (the lockfile and argv above), not on where packets
  can go. A compromised registry package still runs with the sandbox's
  filesystem limits and no scripts.
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
- Baseline failures are not recorded automatically. A profile that omits
  `typecheck`/`build` (the toolchain profile below) leaves the review packet
  silent about them; the CTO review must state "baseline FAIL" explicitly. An
  informational, non-gating check kind would automate that and is not built.
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
      "name": "core-frontend-toolchain",
      "title_prefixes": ["[V2 FE TOOLCHAIN"],
      "sandbox_read_only": ["<home>/.nvm/versions/node/<version>"],
      "require_files": ["frontend/package-lock.json", "frontend/tests/setup.ts"],
      "require_npm": {"dir": "frontend", "scripts": ["test"]},
      "checks": [
        {"name": "install", "argv": ["npm", "--prefix", "frontend", "ci", "--ignore-scripts"], "network": true},
        {"name": "test", "argv": ["npm", "--prefix", "frontend", "run", "test"]},
        {"name": "lint", "argv": ["frontend/node_modules/.bin/oxlint", "frontend/src", "frontend/tests"]}
      ]
    },
    {
      "name": "core-frontend",
      "title_prefixes": ["[V2 FE QA"],
      "sandbox_read_only": ["<home>/.nvm/versions/node/<version>"],
      "require_files": ["frontend/package-lock.json"],
      "require_npm": {"dir": "frontend", "scripts": ["test", "typecheck", "build"]},
      "checks": [
        {"name": "install", "argv": ["npm", "--prefix", "frontend", "ci", "--ignore-scripts"], "network": true},
        {"name": "test", "argv": ["npm", "--prefix", "frontend", "run", "test"]},
        {"name": "lint", "argv": ["frontend/node_modules/.bin/oxlint", "frontend/src", "frontend/tests"]},
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

- Prefixes must not overlap (the validator refuses it), so the toolchain slice
  has its own unique prefix and `core-frontend` is limited to explicit ones.
  Any other frontend title, for example BII-899 `[V2 FE TAILWIND`, matches no
  profile and is refused until a profile for it is defined. That is the
  intended fail-closed behavior.
- `core-frontend-toolchain` is for BII-898, which creates the lockfile, the
  Vitest harness and its smoke test. It deliberately has no `typecheck` or
  `build`: at the pinned base they fail for reasons the card does not own (see
  the measurements below). That is a recorded baseline failure, not a pass.
- `core-frontend` includes `typecheck` and `build`, so every card using it ends
  Blocked until the clean-room import closure lands.
- At the pinned Core base the frontend has no `test` or `typecheck` script, so
  both profiles fail their preconditions until a card adds them. That is the
  intended behavior for BII-898, which owns that toolchain.
- Measured on Fedora at the pinned base `d79518b3` after `npm ci`:
  `npm run build` exits 2 with 192 TypeScript errors (98 are `TS2307`, a
  missing module such as `../components/ui`, `@/components/shadcn/ui/*` or
  `./routes/Landing`; 88 are `TS7006` implicit `any` that follow from them).
  `tsc -b` alone also exits 2.
- `npm run lint` runs `oxlint` with no ignore, so after `npm ci` it scans
  `node_modules` and exits 1 (one error under `node_modules/@tailwindcss`)
  even though `frontend/src` only has a warning. The proposal calls the binary
  directly on `src` and `tests` (exit 0 at the base for `src`).
- `core-backend` needs Docker and PostgreSQL. Without them every test is
  skipped, which the runner counts as a failure.
- No `core-docs` or Lab profiles are proposed: no verified documentation check
  exists, and Lab titles need an explicit prefix decision before a profile can
  be enabled. The existing Lab `setup` (`ln -s`) now runs in the sandbox.
