#!/usr/bin/env bash
# End-to-end test of ccm's auto-switch-on-usage-limit using a fake claude.
# Runs entirely in a throwaway HOME and never touches your real accounts.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
WAIT_SECS="${E2E_WAIT_SECS:-30}"
LIMIT_AFTER="${FAKECLAUDE_LIMIT_AFTER:-2}"
ACCTS=(e2e-a e2e-b e2e-c)

command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 2; }
command -v script >/dev/null || { echo "script(1) is required" >&2; exit 2; }

# The real home comes from the user database, not from $HOME.
REAL_HOME="$(python3 -I -c 'import os,pwd; print(os.path.realpath(pwd.getpwuid(os.getuid()).pw_dir))')"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/ccm-e2e.XXXXXX")"
TMP="$(cd "$TMP" && pwd -P)"
KEEP="${E2E_KEEP:-0}"
cleanup() {
  jobs -p | xargs kill 2>/dev/null || true
  if [ "$KEEP" = 1 ]; then echo "kept $TMP"; else rm -rf "$TMP"; fi
}
trap cleanup EXIT

BIN="$TMP/bin"
FAKE_HOME="$TMP/home"
ROOT="$FAKE_HOME/.claude-accounts"
WORKDIR="$TMP/work/project"
LOG="$TMP/fakeclaude.jsonl"

case "$FAKE_HOME" in
  "$REAL_HOME"|"$REAL_HOME"/*) echo "refusing: throwaway HOME is inside the real home" >&2; exit 2 ;;
esac

# --- build (before HOME is overridden, so the Go caches stay untouched) -----
mkdir -p "$BIN" "$FAKE_HOME" "$WORKDIR"
(cd "$REPO" && go build -o "$BIN/ccm" ./cmd/ccm \
  && go build -o "$BIN/ccm-shim" ./cmd/ccm-shim \
  && go build -o "$BIN/fakeclaude" ./cmd/fakeclaude)

# --- fixtures ----------------------------------------------------------------
mkdir -p "$ROOT"
echo "{}" > "$FAKE_HOME/.claude.json"
python3 -I - "$ROOT" "${ACCTS[@]}" <<'PY'
import json, os, sys, datetime as dt
root, names = sys.argv[1], sys.argv[2:]
now = dt.datetime.now(dt.timezone.utc)
iso = lambda d: d.strftime("%Y-%m-%dT%H:%M:%SZ")
accts, cache = [], []
for n in names:
    d = os.path.join(root, n)
    os.makedirs(d, mode=0o700, exist_ok=True)
    accts.append({"name": n, "dir": d})
def entry(name, used):
    return {"name": name, "email": name + "@example.invalid", "plan": "max",
            "five_hour_used": used, "seven_day_used": used,
            "five_hour_resets_at": iso(now + dt.timedelta(hours=4)),
            "seven_day_resets_at": iso(now + dt.timedelta(days=5)),
            "state": "ok", "fetched_at": iso(now)}
# main is near-full so the fake accounts are always preferred.
for name, used in [("main", 95.0)] + [(n, 10.0 + 5 * i) for i, n in enumerate(names)]:
    cache.append(entry(name, used))
open(os.path.join(root, "accounts.json"), "w").write(json.dumps(accts, indent=2))
os.makedirs(os.path.join(root, "cache"), exist_ok=True)
open(os.path.join(root, "cache", "usage.json"), "w").write(json.dumps({
    "written_at": iso(now), "accounts": cache,
    "combined": {"five_hour_left": 0, "seven_day_left": 0, "counted": len(cache),
                 "available_now": len(cache), "next_reset_at": None}}, indent=2))
open(os.path.join(root, "config.json"), "w").write(json.dumps({"autoSwitch": True}, indent=2))
PY

# Keep a pristine cache: scenario 1 correctly marks the first account limited.
cp "$ROOT/cache/usage.json" "$TMP/usage.pristine.json"

# --- hermetic environment ----------------------------------------------------
export HOME="$FAKE_HOME"
if [ "$(cd "$HOME" && pwd -P)" = "$REAL_HOME" ]; then echo "refusing: HOME is the real home" >&2; exit 2; fi
export CCM_HOME="$ROOT"
export CCM_CLAUDE_BIN="$BIN/fakeclaude"
export FAKECLAUDE_LOG="$LOG"
export FAKECLAUDE_LIMIT_ACCOUNTS="${ACCTS[0]}"
export FAKECLAUDE_LIMIT_AFTER="$LIMIT_AFTER"
export HTTPS_PROXY="http://127.0.0.1:9" HTTP_PROXY="http://127.0.0.1:9" ALL_PROXY="http://127.0.0.1:9"
export TERM="${TERM:-xterm}"
unset CLAUDE_CONFIG_DIR ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN CLAUDE_CODE_OAUTH_TOKEN || true

# --- helpers -------------------------------------------------------------------
# run_bounded SECS cmd... : run in the background, kill after SECS, keep its status.
run_bounded() {
  local secs="$1"; shift
  "$@" &
  local pid=$! waited=0
  while kill -0 "$pid" 2>/dev/null && [ "$waited" -lt "$secs" ]; do sleep 1; waited=$((waited + 1)); done
  if kill -0 "$pid" 2>/dev/null; then
    echo "  timeout after ${secs}s; killing" >&2
    kill -TERM "$pid" 2>/dev/null || true; sleep 1; kill -KILL "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
}

# with_tty cmd... : run cmd under a pseudo-terminal (BSD and util-linux script differ).
with_tty() {
  if [ "$(uname)" = Darwin ]; then
    script -q /dev/null "$@"
  else
    script -qec "$(printf '%q ' "$@")" /dev/null
  fi
}

# check_switch MODE : assert the log shows a switch away from the first account.
# MODE=interactive checks the resume prompt in argv; stream-json checks the
# user_message events the fake received on stdin.
check_switch() {
  python3 -I - "$LOG" "${ACCTS[0]}" "$1" <<'PY'
import json, sys
log, first, mode = sys.argv[1:4]
try:
    rows = [json.loads(l) for l in open(log) if l.strip()]
except FileNotFoundError:
    print("  no fakeclaude log"); sys.exit(1)
runs = [r for r in rows if r.get("mode") == mode]
if not runs:
    print("  no %s invocations" % mode); sys.exit(1)
a = runs[0]
if a["account"] != first:
    print("  first run was on %s, want %s" % (a["account"], first)); sys.exit(1)
sid = a["session_id"]
second = [r for r in runs[1:] if r["account"] != first and r.get("resume") and r["session_id"] == sid]
if not second:
    print("  no resumed run of session %s on another account; runs: %s" %
          (sid, [(r["account"], r["session_id"], r.get("resume")) for r in runs])); sys.exit(1)
b = second[0]
if mode == "interactive":
    ok = any(x.startswith("[ccm]") for x in b["argv"])
else:
    ok = any(r.get("event") == "user_message" and r["pid"] == b["pid"] and r["text"].startswith("[ccm]") for r in rows)
if not ok:
    print("  resume prompt starting with [ccm] not delivered to %s (argv %s)" % (b["account"], b["argv"])); sys.exit(1)
print("  switched %s -> %s, resumed %s, [ccm] prompt delivered" % (first, b["account"], sid))
PY
}

FAILED=0
report() { # name status
  if [ "$2" = 0 ]; then echo "PASS: $1"; else echo "FAIL: $1"; FAILED=1; fi
}

# --- scenario 1: interactive ---------------------------------------------------
: > "$LOG"
SID="$(python3 -I -c 'import uuid; print(uuid.uuid4())')"
echo "== scenario 1: interactive (ccm run ${ACCTS[0]}, session $SID)"
(cd "$WORKDIR" && run_bounded "$WAIT_SECS" with_tty "$BIN/ccm" run "${ACCTS[0]}" --session-id "$SID" "initial prompt") \
  >"$TMP/interactive.out" 2>&1 || true
s=0; check_switch interactive || s=$?
[ "$s" = 0 ] || sed 's/^/  | /' "$TMP/interactive.out" | tail -20
report "interactive auto-switch" "$s"

# --- scenario 2: stream-json via the shim ----------------------------------------
cp "$TMP/usage.pristine.json" "$ROOT/cache/usage.json"
: > "$LOG"
echo "== scenario 2: stream-json (ccm-shim, CLAUDE_CONFIG_DIR=${ACCTS[0]})"
INIT='{"type":"control_request","request_id":"req_1","request":{"subtype":"initialize"}}'
USER='{"type":"user","message":{"role":"user","content":[{"type":"text","text":"please do the thing"}]}}'
stream_session() {
  { printf '%s\n' "$INIT"; sleep 1; printf '%s\n' "$USER"; sleep "$((WAIT_SECS / 2))"; } \
    | CLAUDE_CONFIG_DIR="$ROOT/${ACCTS[0]}" "$BIN/ccm-shim" \
        -p --output-format stream-json --input-format stream-json --verbose
}
(cd "$WORKDIR" && run_bounded "$WAIT_SECS" stream_session) >"$TMP/stream.out" 2>"$TMP/stream.err" || true
s=0; check_switch stream-json || s=$?
[ "$s" = 0 ] || { sed 's/^/  | /' "$TMP/stream.out" | tail -20; sed 's/^/  ! /' "$TMP/stream.err" | tail -10; }
report "stream-json auto-switch" "$s"

exit "$FAILED"
