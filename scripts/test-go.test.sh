#!/usr/bin/env bash
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEST_DIR=$(mktemp -d "${TMPDIR:-/tmp}/multica-test-go.XXXXXX")
BIN_DIR="$TEST_DIR/bin"
CALLS_FILE="$TEST_DIR/go-calls.log"
OUTPUT_FILE="$TEST_DIR/output.log"

cleanup() {
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

mkdir -p "$BIN_DIR"
export MULTICA_TEST_GO_CALLS="$CALLS_FILE"

cat >"$BIN_DIR/go" <<'EOF'
#!/usr/bin/env bash
set -eu

case "${1:-}" in
  run)
    if [ "$#" -ne 2 ] || [ "$2" != "./internal/testdbprobe" ]; then
      echo "unexpected go run arguments: $*" >&2
      exit 2
    fi
    printf '%s\n' "$*" >>"$MULTICA_TEST_GO_CALLS"
    if [ "${MULTICA_TEST_DB_PROBE_RESULT:-reachable}" = unreachable ]; then
      if [ "${MULTICA_TEST_DB_OPTIONAL:-}" = 1 ]; then
        echo "WARNING: MULTICA_TEST_DB_OPTIONAL=1; DB-backed suites did not run"
        exit 0
      fi
      echo "test database unavailable. Remedy: start Postgres or set MULTICA_TEST_DB_OPTIONAL=1" >&2
      exit 1
    fi
    ;;
  list)
    if [ "$#" -ne 2 ] || [ "$2" != "./..." ]; then
      echo "unexpected go list arguments: $*" >&2
      exit 2
    fi
    printf '%s\n' \
      github.com/multica-ai/multica/server \
      github.com/multica-ai/multica/server/internal/daemon \
      github.com/multica-ai/multica/server/pkg/agent \
      github.com/multica-ai/multica/server/pkg/agent/internal/testutil
    ;;
  test)
    printf '%s\n' "$*" >>"$MULTICA_TEST_GO_CALLS"
    ;;
  *)
    echo "unexpected go command: $*" >&2
    exit 2
    ;;
esac
EOF
chmod 755 "$BIN_DIR/go"

PATH="$BIN_DIR:$PATH" bash "$SCRIPT_DIR/test-go.sh" --race

expected_calls='run ./internal/testdbprobe
test -race github.com/multica-ai/multica/server github.com/multica-ai/multica/server/internal/daemon
test -race -p 2 -parallel 2 ./pkg/agent/...'
expected_calls_without_race='run ./internal/testdbprobe
test github.com/multica-ai/multica/server github.com/multica-ai/multica/server/internal/daemon
test -p 2 -parallel 2 ./pkg/agent/...'
actual_calls=$(cat "$CALLS_FILE")
if [ "$actual_calls" != "$expected_calls" ]; then
  echo "unexpected go test calls:" >&2
  printf '%s\n' "$actual_calls" >&2
  exit 1
fi

: >"$CALLS_FILE"
set +e
PATH="$BIN_DIR:$PATH" bash "$SCRIPT_DIR/test-go.sh" --unknown >"$OUTPUT_FILE" 2>&1
status=$?
set -e

if [ "$status" -ne 2 ]; then
  echo "unknown option returned status $status, want 2" >&2
  cat "$OUTPUT_FILE" >&2
  exit 1
fi
if [ -s "$CALLS_FILE" ]; then
  echo "unknown option invoked go:" >&2
  cat "$CALLS_FILE" >&2
  exit 1
fi
if ! grep -q '^usage: .*test-go.sh \[--race\]$' "$OUTPUT_FILE"; then
  echo "unknown option did not print usage" >&2
  cat "$OUTPUT_FILE" >&2
  exit 1
fi

: >"$CALLS_FILE"
set +e
MULTICA_TEST_DB_PROBE_RESULT=unreachable PATH="$BIN_DIR:$PATH" \
  bash "$SCRIPT_DIR/test-go.sh" >"$OUTPUT_FILE" 2>&1
status=$?
set -e

if [ "$status" -eq 0 ]; then
  echo "unreachable database returned status 0, want nonzero" >&2
  cat "$OUTPUT_FILE" >&2
  exit 1
fi
if [ "$(cat "$CALLS_FILE")" != 'run ./internal/testdbprobe' ]; then
  echo "unreachable database did not abort before package discovery:" >&2
  cat "$CALLS_FILE" >&2
  exit 1
fi
if ! grep -q 'Remedy:.*MULTICA_TEST_DB_OPTIONAL=1' "$OUTPUT_FILE"; then
  echo "unreachable database did not name the remedy" >&2
  cat "$OUTPUT_FILE" >&2
  exit 1
fi

: >"$CALLS_FILE"
MULTICA_TEST_DB_PROBE_RESULT=unreachable MULTICA_TEST_DB_OPTIONAL=1 \
  PATH="$BIN_DIR:$PATH" bash "$SCRIPT_DIR/test-go.sh" >"$OUTPUT_FILE" 2>&1
if ! grep -q 'WARNING: MULTICA_TEST_DB_OPTIONAL=1; DB-backed suites did not run' "$OUTPUT_FILE"; then
  echo "opt-out did not print the skip banner" >&2
  cat "$OUTPUT_FILE" >&2
  exit 1
fi
if [ "$(cat "$CALLS_FILE")" != "$expected_calls_without_race" ]; then
  echo "opt-out did not continue to the Go suites:" >&2
  cat "$CALLS_FILE" >&2
  exit 1
fi

echo "test-go.test.sh: PASS"
