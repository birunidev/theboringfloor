#!/usr/bin/env bash
set -euo pipefail
launcher=${1:?Pass the absolute launcher script path}
fixture=$(mktemp -d)
mkdir -p "$fixture/bin"
cat >"$fixture/bin/background" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
name=${0##*/}
printf '%s\n' "$@" >"$TASK_OFFICE_ROOT/$name.args"
printf '%s\n' "$$" >"$TASK_OFFICE_ROOT/$name.pid"
trap 'touch "$TASK_OFFICE_ROOT/$name.stopped"; exit 0' TERM
while :; do sleep 0.05; done
STUB
cp "$fixture/bin/background" "$fixture/bin/officeview"
cp "$fixture/bin/background" "$fixture/bin/autopilot"
cat >"$fixture/bin/tbo-v1" <<'STUB'
#!/usr/bin/env bash
[[ "$1" == --project && "$2" == 'project with spaces' ]] || exit 99
exit 7
STUB
chmod +x "$fixture/bin/officeview" "$fixture/bin/autopilot" "$fixture/bin/tbo-v1"
export TASK_OFFICE_ROOT="$fixture" TASK_OFFICE_BOARD='V2 Engineering'
export TASK_OFFICE_PROJECT='project with spaces' TASK_OFFICE_BIND='127.0.0.1:8089'
export PATH="$fixture/bin:$PATH"
printf 'preserve\n' >"$fixture/STOP"
set +e
bash "$launcher" >"$fixture/output" 2>&1
result=$?
set -e
if [[ "$result" != 7 ]]; then
  cat "$fixture/output" >&2
  echo "FAIL: cockpit exit code $result, expected 7" >&2
  exit 1
fi
[[ -f "$fixture/officeview.stopped" && -f "$fixture/autopilot.stopped" ]]
[[ "$(cat "$fixture/STOP")" == preserve ]]
[[ "$(tail -n 1 "$fixture/autopilot.args")" == 'V2 Engineering' ]]
for name in officeview autopilot; do
  if kill -0 "$(cat "$fixture/$name.pid")" 2>/dev/null; then
    echo "FAIL: $name survived launcher exit" >&2
    exit 1
  fi
done
echo 'PASS: board/project quoting, cockpit exit code, UI/worker cleanup, STOP preserved; no live worker used'
