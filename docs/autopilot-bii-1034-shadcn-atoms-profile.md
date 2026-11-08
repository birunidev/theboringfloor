# BII-1034: the `core-frontend-shadcn-atoms` profile for BII-900

Status as of 2026-11-08: **prepared and verified off-line, not applied to the live runner.** This file records
what was checked so it can be reviewed without access to the Fedora host. It contains no host paths or
credentials; `<home>` stands for the runner owner's home directory.

BII-900 (`[V2 FE SHADCN-ATOMS]`, four clean-room shadcn primitives and a test) cannot run under the four
installed profiles: nothing routes its title prefix, and a broad `[V2 FE` route would defeat the fail-closed
profile selection. This profile gives it its own, narrow route.

## The profile

Added as the last profile of the V2 board. Everything else in the private config stays byte-identical.

```json
{
  "name": "core-frontend-shadcn-atoms",
  "title_prefixes": ["[V2 FE SHADCN-ATOMS]"],
  "sandbox_read_only": ["<home>/.nvm/versions/node/<version>"],
  "allowed_paths": [
    "frontend/src/components/shadcn/ui/badge.tsx",
    "frontend/src/components/shadcn/ui/button.tsx",
    "frontend/src/components/shadcn/ui/card.tsx",
    "frontend/src/components/shadcn/ui/input.tsx",
    "frontend/tests/shadcn-atoms.test.tsx"
  ],
  "require_files": [
    "frontend/package-lock.json",
    "frontend/vitest.config.ts",
    "frontend/tests/shadcn-atoms.test.tsx"
  ],
  "require_npm": {"dir": "frontend", "scripts": ["test", "typecheck"]},
  "checks": [
    {"name": "install", "argv": ["npm", "--prefix", "frontend", "ci", "--ignore-scripts"], "network": true},
    {"name": "shadcn atoms test",
     "argv": ["npm", "--prefix", "frontend", "run", "test", "--", "tests/shadcn-atoms.test.tsx"],
     "require_output": "(?m)^\\s*Tests\\s+[1-9][0-9]* passed",
     "reject_output": "(?m)^\\s*Tests\\s.*\\b(skipped|todo|failed)\\b"},
    {"name": "lint",
     "argv": ["frontend/node_modules/.bin/oxlint", "frontend/src/components/shadcn/ui", "frontend/tests/shadcn-atoms.test.tsx"]}
  ]
}
```

It owns exactly the five files. It does not enable lockfile generation: the lockfile already exists on the
staging branch (BII-898), and `package.json`, the lockfile and `vitest.config.ts` are not owned, so an agent
cannot change them. It does not run the global `build` or `typecheck`, which fail at the pinned base for
reasons these files do not own (about 192 TypeScript errors); that is a recorded baseline failure, not a pass.

## The staging base

The V2 board `base` moves from the Core pin to `cto/autopilot-fe-foundations`, a non-release development
branch: BII-898 (reviewed draft PR #9, `e9cfc424`) plus the three reviewed BII-899 files (PR #8, `510be500`).
Verified at `3bb55cb207fa43d92d4fb8c1c63042b125d49832`: it descends from `e9cfc424`, adds exactly
`frontend/src/styles/app.css`, `frontend/src/styles/shadcn.css` and `frontend/tests/css-contract.test.mjs`,
and their git blob SHAs are `774e0cf7`, `8f79849e` and `c9c3de7c`.

The base applies to both lanes of the board, so the Backend lane must stay empty while it is in place, and
draft PRs opened by autopilot cards target this branch, not the Core pin. No runner code change is needed: a
base name containing `/` passes the branch check, and publishing only passes it to `gh pr create --base`.

## What was verified

Real sandbox, real npm and registry, on a clone of the staging branch:

- strict `npm ci --ignore-scripts`: pass, 282 packages (the BII-898 lockfile passed the registry and integrity
  preflight); `npm run test`: pass, 6 tests; `node --test --test-reporter=tap` on the BII-899 contract: 10 of
  10; scoped oxlint: 0 errors. Global build and typecheck were not run and are not claimed.
- the profile's mechanics, using a BII-900-like fixture written for the probe (not product code): install,
  test (6 real assertions) and lint pass; a skipped plus todo test, a failing assertion, an empty test file and
  a missing test file are each rejected; ownership refuses package.json, vitest.config.ts, the lockfile, an
  extra atom, `src/lib/utils.ts`, the BII-899 CSS, the BII-898 smoke test and a backend file.
- routing against the staged config: the BII-900 title routes here; the toolchain, Tailwind, QA and backend
  titles route as before; `[V2 FE ROUTES]` and a prefix missing its closing bracket are refused.

A test in this repository parses the JSON block above and checks these properties, so the example cannot drift.

## Two pitfalls for the BII-900 spec

Both were found by the probe, and each costs a failed run if the agent meets it:

1. **No `@/` alias under Vitest.** `frontend/vitest.config.ts` (owned by BII-898) defines no alias; the alias
   lives only in `vite.config.ts`, which Vitest ignores once a `vitest.config.ts` exists. An import such as
   `@/components/shadcn/ui/button` fails with "Failed to resolve import" and the run reports no tests. The atoms
   and the test must use relative imports; `cn` is exported by `frontend/src/lib/utils.ts`.
2. **No automatic DOM cleanup.** `frontend/tests/setup.ts` only imports `@testing-library/jest-dom/vitest`, and
   the config sets no globals, so Testing Library does not clean up between tests. Without `afterEach(cleanup)`
   in the test file, later tests fail with "Found multiple elements with the role button".

`vitest.config.ts` already includes `tests/**/*.test.tsx` and uses jsdom, so a `.tsx` test is collected.

## Applying it (needs the owner's authorization)

The runner binary does not change; only the private config does.

1. Stop the worker with the launcher and wait for it to exit; STOP stays set.
2. Build the new config from the live one: append the profile above (Node path filled in) and set the V2 board
   `base`. Confirm the difference from the live config is exactly that, and that the existing four profiles, the
   Lab board and the caps (250000 per card, 1000000 per day) are unchanged.
3. Validate it with the installed runner's loader, which stops at the board filter before any lock, budget,
   Space or model call: `autopilot --config <staged> --dry-run --board __no_such_board__` must print
   `no board "__no_such_board__" in config`.
4. Back up the config (mode 700 directory, 600 file), swap by rename, re-verify the hash, the loader, the base,
   the profile list, caps, file mode, STOP, no worker and an unchanged ledger. Any failure restores the backup.
5. Release STOP and start exactly one worker, then admit BII-900 once, after its spec carries the two pitfalls.

Rollback: restore the backed-up config and compare the recorded hash. The binary was never replaced.
