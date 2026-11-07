#!/usr/bin/env bash
set -euo pipefail
umask 077

cd "${TASK_OFFICE_ROOT:?Taskfile must supply the repository root}"
board=${TASK_OFFICE_BOARD:-V2 Engineering}
project=${TASK_OFFICE_PROJECT:-$PWD}
bind=${TASK_OFFICE_BIND:-127.0.0.1:8080}
logs=$(mktemp -d "${TMPDIR:-/tmp}/autopilot-office.XXXXXXXX")
ui_pid=''
worker_pid=''

cleanup() {
  trap - EXIT INT TERM
  for pid in "$worker_pid" "$ui_pid"; do
    if [[ -n "$pid" ]]; then kill -TERM "$pid" 2>/dev/null || true; fi
  done
  for pid in "$worker_pid" "$ui_pid"; do
    if [[ -n "$pid" ]]; then wait "$pid" 2>/dev/null || true; fi
  done
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf 'Graphical UI: http://%s\nBoard: %s\nLogs: %s\n' "$bind" "$board" "$logs"
printf 'Exit the cockpit to stop this launcher’s UI and worker. Existing STOP is preserved.\n'
bin/officeview --bind "$bind" --controls >"$logs/ui.log" 2>&1 &
ui_pid=$!
sleep 0.2
if ! kill -0 "$ui_pid" 2>/dev/null; then
  printf 'UI exited during startup. See %s/ui.log\n' "$logs" >&2
  exit 1
fi
bin/autopilot --live --board "$board" >"$logs/worker.log" 2>&1 &
worker_pid=$!
sleep 0.2
if ! kill -0 "$worker_pid" 2>/dev/null; then
  printf 'Worker exited during startup (STOP may be set). See %s/worker.log\n' "$logs" >&2
fi
# Only the cockpit gets the wrapper's v1 PATH; the worker keeps opencode v2.
tbo-v1 --project "$project"
