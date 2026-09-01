#!/usr/bin/env bash
set -euo pipefail

# `make build` has to name its outputs the way the *target* platform expects.
# Windows refuses to execute an extensionless file, so a Windows source build
# whose artifacts are named `multica` produces a CLI that cannot re-exec itself
# as a daemon (#7255) — the build succeeds and the failure surfaces later as a
# misleading "not found" at startup.
#
# The suffix is derived from GOOS, which reaches a build two ways: as an
# environment variable and as a Make variable on the command line. `go build`
# honors both, so a suffix that honors only one silently rebuilds the original
# bug. Nothing else covers this: the Go test suite never runs the Makefile, and
# CI's own build steps call `go build` directly.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

fail() {
  echo "$1" >&2
  exit 1
}

# The recipe reads `-o bin/server$(EXE) ./cmd/server`, so the trailing space is
# what keeps an expected `bin/server` from matching an emitted `bin/server.exe`.
require_outputs() {
  local label=$1 suffix=$2 output=$3 binary

  for binary in server multica migrate; do
    grep -Fq -- "-o bin/${binary}${suffix} " <<<"$output" ||
      fail "$label: expected 'go build ... -o bin/${binary}${suffix}', got:
$output"
  done
}

# A `go` shim that records every invocation, so the assertions below can tell
# "the Makefile probed the toolchain" from "the Makefile did not" without
# depending on how the host PATH is laid out.
probe_dir="$(mktemp -d)"
trap 'rm -rf "$probe_dir"' EXIT
real_go="$(command -v go || true)"
cat >"$probe_dir/go" <<EOF
#!/usr/bin/env bash
echo "\$@" >>"$probe_dir/invocations"
[ -n "$real_go" ] || exit 1
exec "$real_go" "\$@"
EOF
chmod +x "$probe_dir/go"

probe_count() {
  [ -f "$probe_dir/invocations" ] || { echo 0; return; }
  wc -l <"$probe_dir/invocations" | tr -d ' '
}

require_outputs "GOOS=windows in the environment" .exe \
  "$(GOOS=windows make -n build)"
require_outputs "GOOS=windows as a Make variable" .exe \
  "$(make -n build GOOS=windows)"
require_outputs "GOOS=linux in the environment" "" \
  "$(GOOS=linux make -n build)"
require_outputs "GOOS=darwin as a Make variable" "" \
  "$(make -n build GOOS=darwin)"

# Non-build targets must not reach for a Go toolchain: `make help` and
# `make clean` are the first thing a frontend-only contributor runs, and a
# global suffix assignment makes every one of them print `go: Command not
# found` on a checkout with no Go installed.
PATH="$probe_dir:$PATH" make -n clean >/dev/null
PATH="$probe_dir:$PATH" make help >/dev/null
[ "$(probe_count)" = 0 ] ||
  fail "non-build targets invoked go $(probe_count) time(s): $(cat "$probe_dir/invocations")"

# With no GOOS given, the suffix has to follow the toolchain's own default.
if [ -n "$real_go" ]; then
  host_suffix=""
  [ "$("$real_go" env GOOS)" = windows ] && host_suffix=.exe
  require_outputs "no GOOS set" "$host_suffix" \
    "$(PATH="$probe_dir:$PATH" make -n build)"
  [ "$(probe_count)" != 0 ] ||
    fail "no GOOS set: expected the build target to resolve GOOS via go env"
else
  echo "skipping the host-default case: no go toolchain on PATH"
fi

# A shell-exported PostgreSQL port is an explicit caller choice. The checked-in
# env file must not silently replace it during Make's include phase.
postgres_port="$(
  POSTGRES_PORT=25432 make -s ENV_FILE=.env.example \
    --eval 'print-postgres-port:;@echo $(POSTGRES_PORT):$(MULTICA_POSTGRES_PORT_OVERRIDE)' print-postgres-port
)"
[ "$postgres_port" = 25432:25432 ] ||
  fail "POSTGRES_PORT=25432 make ... resolved to $postgres_port"

# A direct Compose target must refuse a legacy env file that carries an
# isolated PostgreSQL port without its Compose project identity. The Docker
# shim makes this assertion non-vacuous: reaching it records the unsafe call.
identity_env="$probe_dir/keyless-compose.env"
docker_marker="$probe_dir/docker-invocation"
cat >"$identity_env" <<'EOF'
POSTGRES_PORT=26106
EOF
cat >"$probe_dir/docker" <<EOF
#!/usr/bin/env bash
printf 'project=%s|port=%s|args=%s\n' \
  "\${COMPOSE_PROJECT_NAME:-<unset>}" "\${POSTGRES_PORT:-<unset>}" "\$*" >"$docker_marker"
EOF
chmod +x "$probe_dir/docker"

status=0
identity_output="$(
  PATH="$probe_dir:$PATH" make -s ENV_FILE="$identity_env" db-up 2>&1
)" || status=$?
[ "$status" -ne 0 ] || fail "db-up accepted a port-bearing env without COMPOSE_PROJECT_NAME"
[ ! -e "$docker_marker" ] || fail "db-up reached Docker without a Compose identity: $(cat "$docker_marker")"
grep -Fq "Refusing Compose: $identity_env carries POSTGRES_PORT but no COMPOSE_PROJECT_NAME." \
  <<<"$identity_output" || fail "db-up refusal did not name the broken env file: $identity_output"
grep -Fq "Remediation:" <<<"$identity_output" ||
  fail "db-up refusal did not include a remediation command: $identity_output"

echo "✓ Makefile build and Compose identity behavior verified"
