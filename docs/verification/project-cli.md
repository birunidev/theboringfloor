# Project CLI verification (2026-11-07)

Changes: `cmd/autopilot/main.go`, new `cmd/autopilot/projects{,_test}.go`,
new `internal/autopilot/projects/projects{,_test}.go`, README and project guide.

Commands ran in `/tmp/theboringfloor-project-cli` on the founder's machine.
The checks used bubblewrap with no network, read-only source/module cache,
empty home and private writable tmp/cache:

```sh
bwrap --die-with-parent --unshare-net --ro-bind / / --dev /dev --proc /proc \
  --tmpfs /tmp --tmpfs /home/birunidev \
  --ro-bind /tmp/theboringfloor-project-cli /tmp/theboringfloor-project-cli \
  --ro-bind /home/birunidev/go/pkg/mod /home/birunidev/go/pkg/mod \
  --chdir /tmp/theboringfloor-project-cli --setenv HOME /home/birunidev \
  --setenv GOCACHE /tmp/go-cache --setenv GOMODCACHE /home/birunidev/go/pkg/mod \
  --setenv GOPROXY off -- /usr/bin/sh -c \
  'go vet ./internal/autopilot/... ./cmd/autopilot/ && go test -race -count=1 ./internal/autopilot/... ./cmd/autopilot/'
```

Exit 0; vet emitted no diagnostics. Actual test output:

```text
ok  	github.com/theboringhumane/theboringfloor/internal/autopilot/board	1.028s
ok  	github.com/theboringhumane/theboringfloor/internal/autopilot/engine	1.489s
ok  	github.com/theboringhumane/theboringfloor/internal/autopilot/handoff	1.019s
ok  	github.com/theboringhumane/theboringfloor/internal/autopilot/ocv2	1.099s
ok  	github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline	1.024s
ok  	github.com/theboringhumane/theboringfloor/internal/autopilot/projects	1.055s
ok  	github.com/theboringhumane/theboringfloor/internal/autopilot/publish	1.239s
ok  	github.com/theboringhumane/theboringfloor/internal/autopilot/runstate	1.017s
ok  	github.com/theboringhumane/theboringfloor/internal/autopilot/spaceboard	1.035s
ok  	github.com/theboringhumane/theboringfloor/internal/autopilot/worktree	1.302s
ok  	github.com/theboringhumane/theboringfloor/cmd/autopilot	1.106s
```

Tests cover temporary repositories/configuration, existing aliases, argv preservation,
preview without saving, malformed input, missing policy/lists, ambiguous lists,
Done ID aliases, active worker lock, concurrent config edits, config mode/settings,
and unchanged STOP/usage/status. Fake boards recorded no moves/comments.

Build:

```sh
go build -o /home/birunidev/Documents/projects/theboringfloor/bin/autopilot-next ./cmd/autopilot
```

Exit 0, no output. Separate binary preserves the active worker executable.
`autopilot-next project list` exit 0, actual output:

```text
PROJECT / BOARD  REPOSITORY       BASE                   CHECKS  PATH
V2 Engineering   birunidev-space  bii-504-slice-qa       2       /home/birunidev/Documents/projects/birunidev-official/birunidev-space
Lab Engineering  birunidev-lab    lab/backend-sprint-v2  3       /home/birunidev/Documents/projects/birunidev-official/birunidev-lab
```

Real `project check birunidev-space` and `project check birunidev-lab` both exited 0.
They resolved the expected board IDs, bases and Autopilot Queue, with respectively
2 and 3 checks and 1 policy card each. Both ended with this actual output:

```text
Metadata ready. Commands were not executed. Card authorization and founder holds are still checked by pre-flight.
```

`git diff --check`: exit 0, no output.

Limits: no new real project was registered, no live/dry model run was launched,
and no verification command from the real project configs was executed. Existing
worker and saved configuration were left in place. Metadata checks do not prove
GitHub authentication, sandbox dependency completeness or card authorization.
Space OAuth may refresh through the existing client. Projects requiring setup
commands still configure those explicitly in JSON. CLI-only change; no visual
assets or UI styles changed.

## UI address follow-up

`project run` now prints `UI: http://127.0.0.1:8080 (officeview must be running separately)`.
`--ui-port` selects a custom port and rejects values outside 1..65535.
Tests assert default/custom URL output for run plans and invalid-port rejection.

Using the bubblewrap command above with
`go vet ./cmd/autopilot/ && go test -race -count=1 ./cmd/autopilot/`
exited 0; vet was silent, actual test output:

```text
ok  github.com/theboringhumane/theboringfloor/cmd/autopilot  1.104s
```

Preview binary rebuilt successfully; `project help` displays the new option.
`git diff --check` exited 0 without output. A sandbox-local curl probe could not
connect to port 8080 (HTTP 000); no claim is made that a UI server is running.
Printing the URL does not start the UI or verify availability.
