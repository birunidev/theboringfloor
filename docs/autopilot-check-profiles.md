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
- `require_output` / `reject_output` that are not valid regular expressions;
- `allowed_paths` that are empty, absolute, contain `..`, or name the whole
  repository (`**`, `*`, `.`): list the files the card owns;
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
- **Source ownership comes from config.** `allowed_paths` is the exact set of
  files a card on the profile may change. It is checked against the worktree's
  real changes (committed and untracked) before any check runs, and a violation
  is a failed check named `profile source ownership`. The existing scope gate
  still applies, but its scope is whatever the agent's pre-flight wrote; this
  one cannot be widened by the agent.
- **Output assertions.** An exit code is not enough evidence. `node:test` exits
  0 for an empty test file (Node 24.16 reports it as one passing test) and for
  skipped tests, and Vitest 5.1 exits 0 for `1 passed | 1 skipped | 1 todo`.
  `require_output` must match and `reject_output` must not, so a profile can
  demand a minimum test count and no skipped or todo tests. Matching and the
  reported output use plain text: ANSI colour codes are stripped first, because
  Vitest prints them even with `CI=1`.
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
  - the lockfile is exactly version 2 or 3 (any other version, including a
    future 4, is refused until it has been evaluated), has no `link` entries,
    and every package
    resolves from `https://registry.npmjs.org/` and carries a sha512/sha384
    integrity hash.
  The sandbox also sets `npm_config_registry`, `ignore_scripts`, a private
  cache, and `userconfig`/`globalconfig` to two different files that do not
  exist in the sandbox's private `/tmp` (`/tmp/npm-userconfig`,
  `/tmp/npm-globalconfig`; npm treats a missing config as empty but refuses the
  same path for both levels, so `/dev/null` cannot be used), with `$HOME` empty.
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
migrate first. Paths are placeholders. This block is parsed and validated by
`TestDocsMigrationExampleIsValid`, so it cannot drift from the code.

```json
{
  "board_id": "board_938353e08e72",
  "name": "V2 Engineering",
  "checks": [],
  "sandbox_read_only": [
    "<uv python dir>",
    "<venv dir>"
  ],
  "profiles": [
    {
      "name": "core-frontend-toolchain",
      "title_prefixes": [
        "[V2 FE TOOLCHAIN"
      ],
      "sandbox_read_only": [
        "<home>/.nvm/versions/node/<version>"
      ],
      "allowed_paths": [
        "frontend/package.json",
        "frontend/package-lock.json",
        "frontend/vite.config.ts",
        "frontend/vitest.config.ts",
        "frontend/tests/setup.ts",
        "frontend/tests/toolchain-smoke.test.ts"
      ],
      "require_files": [
        "frontend/package-lock.json",
        "frontend/vitest.config.ts",
        "frontend/tests/setup.ts",
        "frontend/tests/toolchain-smoke.test.ts"
      ],
      "require_npm": {
        "dir": "frontend",
        "scripts": [
          "test",
          "typecheck"
        ]
      },
      "checks": [
        {
          "name": "install",
          "argv": [
            "npm",
            "--prefix",
            "frontend",
            "ci",
            "--ignore-scripts"
          ],
          "network": true
        },
        {
          "name": "test",
          "argv": [
            "npm",
            "--prefix",
            "frontend",
            "run",
            "test"
          ],
          "require_output": "(?m)^\\s*Tests\\s+[1-9][0-9]* passed",
          "reject_output": "(?m)^\\s*Tests\\s.*\\b(skipped|todo|failed)\\b"
        },
        {
          "name": "lint",
          "argv": [
            "frontend/node_modules/.bin/oxlint",
            "frontend/src",
            "frontend/tests"
          ]
        }
      ]
    },
    {
      "name": "core-frontend-tailwind",
      "title_prefixes": [
        "[V2 FE TAILWIND"
      ],
      "sandbox_read_only": [
        "<home>/.nvm/versions/node/<version>"
      ],
      "allowed_paths": [
        "frontend/src/styles/app.css",
        "frontend/src/styles/shadcn.css",
        "frontend/tests/css-contract.test.mjs"
      ],
      "require_files": [
        "frontend/src/styles/app.css",
        "frontend/src/styles/shadcn.css",
        "frontend/tests/css-contract.test.mjs"
      ],
      "checks": [
        {
          "name": "css source contract",
          "argv": [
            "node",
            "--test",
            "--test-reporter=tap",
            "frontend/tests/css-contract.test.mjs"
          ],
          "require_output": "(?m)^# tests ([6-9]|[1-9][0-9]+)$",
          "reject_output": "(?m)^# (skipped|todo|cancelled|fail) [1-9]"
        },
        {
          "name": "tailwind imported exactly once",
          "argv": [
            "grep",
            "-cE",
            "^@import ['\\\"]tailwindcss['\\\"]",
            "frontend/src/styles/app.css"
          ],
          "require_output": "(?m)^1$"
        }
      ]
    },
    {
      "name": "core-frontend",
      "title_prefixes": [
        "[V2 FE QA"
      ],
      "sandbox_read_only": [
        "<home>/.nvm/versions/node/<version>"
      ],
      "require_files": [
        "frontend/package-lock.json"
      ],
      "require_npm": {
        "dir": "frontend",
        "scripts": [
          "test",
          "typecheck",
          "build"
        ]
      },
      "checks": [
        {
          "name": "install",
          "argv": [
            "npm",
            "--prefix",
            "frontend",
            "ci",
            "--ignore-scripts"
          ],
          "network": true
        },
        {
          "name": "test",
          "argv": [
            "npm",
            "--prefix",
            "frontend",
            "run",
            "test"
          ],
          "require_output": "(?m)^\\s*Tests\\s+[1-9][0-9]* passed",
          "reject_output": "(?m)^\\s*Tests\\s.*\\b(skipped|todo|failed)\\b"
        },
        {
          "name": "lint",
          "argv": [
            "frontend/node_modules/.bin/oxlint",
            "frontend/src",
            "frontend/tests"
          ]
        },
        {
          "name": "typecheck",
          "argv": [
            "npm",
            "--prefix",
            "frontend",
            "run",
            "typecheck"
          ]
        },
        {
          "name": "build",
          "argv": [
            "npm",
            "--prefix",
            "frontend",
            "run",
            "build"
          ]
        }
      ]
    },
    {
      "name": "core-backend",
      "title_prefixes": [
        "[V2 BE",
        "[V2 API"
      ],
      "checks": [
        {
          "name": "compile",
          "argv": [
            "<venv>/bin/python",
            "-m",
            "compileall",
            "-q",
            "backend/app",
            "backend/tests"
          ]
        },
        {
          "name": "core slice tests",
          "argv": [
            "<venv>/bin/python",
            "-m",
            "pytest",
            "-q",
            "-p",
            "no:cacheprovider",
            "backend/tests/test_core_slice_local.py"
          ]
        }
      ]
    }
  ]
}
```

Notes:

- Prefixes must not overlap (the validator refuses it), so each frontend slice
  has its own unique prefix. Any other frontend title, for example a new
  `[V2 FE ...` slice, matches no profile and is refused until a profile for it
  is defined. That is the intended fail-closed behavior.
- **`core-frontend-toolchain` (BII-898)** owns exactly six files, requires the
  lockfile, Vitest config, setup and smoke test to exist, requires `test` and
  `typecheck` scripts to exist, installs from the lockfile in the sandbox, runs
  Vitest (at least one passing test; no skipped, todo or failed), and lints
  `frontend/src` and `frontend/tests`. It deliberately does not run `typecheck`
  or `build`: at the pinned base they fail for reasons the card does not own
  (below). That is a recorded **baseline FAIL**, never a pass. Whether the
  smoke test is *meaningful* (React, Query, Router and Axios actually
  exercised) is a CTO review judgment; a machine can only require that real
  tests ran.
- **`core-frontend-tailwind` (BII-899)** owns exactly `app.css`, `shadcn.css`
  and `css-contract.test.mjs`, and needs no packages, so it does not depend on
  BII-898's lockfile. It runs the card's `node:test` file with the TAP reporter,
  requires at least six tests and no skipped, todo, cancelled or failed ones
  (an empty file counts as one test and is refused), and independently checks
  with `grep` that `@import 'tailwindcss'` appears exactly once in `app.css`.
  The contract test itself, which the card's agent writes, must assert the
  acceptance criteria: exactly one Tailwind import and no second one in
  `shadcn.css`; no import of landing, waiting-list or other legacy styles and
  no remote `url(`; the semantic variables (`--background`, `--foreground`,
  `--primary`, `--primary-foreground`, `--muted`, `--border`, `--ring`,
  `--radius`) are defined; `--background` is white; Plus Jakarta Sans is the
  sans family; a `:focus-visible` rule uses `--ring`; and the checker function
  rejects a fixture with a duplicate import and one with a forbidden import.
  No `build` check: the build fails at the base.
- `core-frontend` includes `typecheck` and `build`, so every card using it ends
  Blocked until the clean-room import closure lands.
- Both frontend slice profiles need `node` on the `PATH` of the shell that
  starts the worker, and the nvm version directory in `sandbox_read_only`.
- Measured on Fedora at the pinned base `d79518b3` after `npm ci`:
  `npm run build` exits 2 with 192 TypeScript errors (98 are `TS2307`, a
  missing module such as `../components/ui`, `@/components/shadcn/ui/*` or
  `./routes/Landing`; 88 are `TS7006` implicit `any` that follow from them).
  `tsc -b` alone also exits 2.
- `npm run lint` runs `oxlint` with no ignore, so after `npm ci` it scans
  `node_modules` and exits 1 (one error under `node_modules/@tailwindcss`)
  even though `frontend/src` only has a warning. The profiles call the binary
  directly on `src` and `tests`.
- `core-backend` needs Docker and PostgreSQL. Without them every test is
  skipped, which the runner counts as a failure.
- No `core-docs` or Lab profiles are proposed: no verified documentation check
  exists, and Lab titles need an explicit prefix decision before a profile can
  be enabled. The existing Lab `setup` (`ln -s`) now runs in the sandbox.
