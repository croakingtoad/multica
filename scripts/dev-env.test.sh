#!/usr/bin/env bash
# Registry-level behaviour of scripts/dev-env.sh, with no services started.
#
# Everything here runs against a throwaway MULTICA_DEV_HOME holding hand-written
# manifests, so the verbs are exercised end to end without a database, a
# backend, or a port.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

export MULTICA_DEV_HOME="$tmp_dir/dev"
export MULTICA_DEV_WORKSPACES_PARENT="$tmp_dir/workspaces-parent"
export MULTICA_DEV_DESKTOP_APP_DATA="$tmp_dir/app-data"
export MULTICA_DEV_PROFILES_HOME="$tmp_dir/profiles"
export MULTICA_DEV_TMPDIR="$tmp_dir/dev-tmp"

fake_bin="$tmp_dir/bin"
mkdir -p "$fake_bin"
docker_log="$tmp_dir/docker.log"
export DOCKER_LOG="$docker_log"
cat > "$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash
printf 'project=%s port=%s args=%s\n' \
  "${COMPOSE_PROJECT_NAME:-<unset>}" "${POSTGRES_PORT:-<unset>}" "$*" >> "$DOCKER_LOG"
if [ "${FAIL_COMPOSE_DOWN:-0}" = 1 ]; then
  echo "simulated Compose teardown failure" >&2
  exit 42
fi
EOF
cat > "$fake_bin/psql" <<'EOF'
#!/usr/bin/env bash
case " $* " in
  *" DROP DATABASE "*) [ "${FAIL_DROP:-0}" != 1 ] ;;
  *) printf '1\n' ;;
esac
EOF
chmod +x "$fake_bin/docker" "$fake_bin/psql"
export PATH="$fake_bin:$PATH"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

require_contains() {
  local file=$1 expected=$2
  if ! grep -Fq "$expected" "$file"; then
    echo "Expected output to contain: $expected" >&2
    echo "Observed:" >&2
    sed 's/^/  /' "$file" >&2
    exit 1
  fi
}

dev_env() {
  bash "$root_dir/scripts/dev-env.sh" "$@"
}

write_manifest() {
  local name=$1 dir=$2 offset=$3 owner=${4:-agent} compose_project=${5:-} postgres_port=${6:-5432}
  local profile="dev-dev-env-test-$offset"
  mkdir -p "$MULTICA_DEV_HOME/envs/$name/logs"
  cat > "$MULTICA_DEV_HOME/envs/$name/manifest.env" <<EOF
NAME=$name
DIR=$(printf '%q' "$dir")
CREATED_AT=2026-01-01T00:00:00Z
OWNER=$owner
TTL_HOURS=0
ENV_FILE=.env.example
OFFSET=$offset
BACKEND_PORT=$((18080 + offset))
FRONTEND_PORT=$((13000 + offset))
DB_NAME=multica_dev_env_test_$offset
DATABASE_URL=postgres://multica:multica@localhost:$postgres_port/multica_dev_env_test_$offset?sslmode=disable
POSTGRES_PORT=$postgres_port
COMPOSE_PROJECT_NAME=$compose_project
PROFILE=$profile
WORKSPACES_ROOT=$(printf '%q' "$MULTICA_DEV_WORKSPACES_PARENT/multica_workspaces_$profile")
DESKTOP_RENDERER_PORT=$((5174 + offset))
DESKTOP_APP_SUFFIX=$name
EOF
}

out="$tmp_dir/out"

# ---------------------------------------------------------------------------
# An empty registry is a normal state, not an error.
# ---------------------------------------------------------------------------
dev_env list > "$out" 2>&1 || fail "list on an empty registry must succeed"
require_contains "$out" "No environments registered"

dev_env list --json > "$out" 2>&1 || fail "list --json on an empty registry must succeed"
if [ "$(cat "$out")" != "[]" ]; then
  fail "list --json on an empty registry = $(cat "$out"), want []"
fi

# ---------------------------------------------------------------------------
# Manifest serialization and user-provided names are safe. A manifest is
# sourced by Bash, so values must be shell-escaped and a name must never be
# able to walk outside envs/ before destroy eventually runs rm -rf.
# ---------------------------------------------------------------------------
quoted="$tmp_dir/quoted.env"
dangerous='a path with spaces;$(touch should-not-exist)'
bash -c 'source "$1"; write_manifest_value DIR "$2"' _ "$root_dir/scripts/dev-env.sh" "$dangerous" > "$quoted"
loaded="$(bash -c 'source "$1"; printf %s "$DIR"' _ "$quoted")"
[ "$loaded" = "$dangerous" ] || fail "manifest value did not round-trip safely"
[ ! -e "$root_dir/should-not-exist" ] || fail "loading a manifest executed its value"

status=0
dev_env up --name ../../escape > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "up accepted a path-traversing environment name"
require_contains "$out" "Invalid environment name"

status=0
dev_env up --ttl nope > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "up accepted a non-numeric TTL"
require_contains "$out" "TTL must be a positive integer"

# Rewriting an allocated database name must preserve the existing connection
# endpoint, credentials and query parameters.
rewritten="$(bash -c 'source "$1"; database_url_with_name "$2" "$3"' _ \
  "$root_dir/scripts/dev-env.sh" \
  'postgres://dev:p%40ss@127.0.0.1:55432/old_db?sslmode=require&application_name=dev' \
  'new_db')"
[ "$rewritten" = 'postgres://dev:p%40ss@127.0.0.1:55432/new_db?sslmode=require&application_name=dev' ] \
  || fail "database URL rewrite changed more than the database name: $rewritten"

isolated_settings="$(bash -c '
  source "$1"
  printf "%s|%s|%s" \
    "$(postgres_port_for_offset 321)" \
    "$(compose_project_for_name ephemeral-321)" \
    "$(database_url_with_port_and_name "$2" 25753 dev_db)"
' _ "$root_dir/scripts/dev-env.sh" \
  'postgres://dev:p%40ss@127.0.0.1:5432/old_db?sslmode=require')"
[ "$isolated_settings" = '25753|multica_ephemeral-321|postgres://dev:p%40ss@127.0.0.1:25753/dev_db?sslmode=require' ] \
  || fail "ephemeral PostgreSQL settings were not derived together: $isolated_settings"

# The isolated Compose identity must travel in the same checkout env file as
# its allocated PostgreSQL port. Direct Make targets source this file without
# going through dev-env.sh or its manifest.
carrier_root="$tmp_dir/carrier-root"
carrier_env="$carrier_root/.env.worktree"
mkdir -p "$carrier_root"
cat > "$carrier_env" <<'EOF'
POSTGRES_DB=old_db
POSTGRES_PORT=5432
DATABASE_URL=postgres://dev:dev@127.0.0.1:5432/old_db?sslmode=disable
EOF
bash -c '
  source "$1"
  REPO_ROOT="$2"
  DATABASE_URL="$3"
  rewrite_database_endpoint .env.worktree 25753 isolated_db multica_isolated-321
' _ "$root_dir/scripts/dev-env.sh" "$carrier_root" \
  'postgres://dev:dev@127.0.0.1:5432/old_db?sslmode=disable'
grep -Fxq 'POSTGRES_PORT=25753' "$carrier_env" \
  || fail "isolated PostgreSQL port did not reach the checkout env file"
grep -Fxq 'COMPOSE_PROJECT_NAME=multica_isolated-321' "$carrier_env" \
  || fail "isolated Compose project did not reach the checkout env file"
redacted="$(bash -c 'source "$1"; redact_database_url "$2"' _ \
  "$root_dir/scripts/dev-env.sh" 'postgres://dev:real-secret@127.0.0.1:5432/dev')"
case "$redacted" in
  *real-secret*) fail "database URL diagnostics exposed the password" ;;
  'postgres://dev:REDACTED@127.0.0.1:5432/dev') ;;
  *) fail "database URL redaction returned $redacted" ;;
esac

# A pgx probe is mandatory even when neither host PostgreSQL client is on PATH.
# Only its dedicated "connection refused" result may enter the Compose fallback;
# authentication, timeout, build and unknown failures all fail closed.
probe_root="$tmp_dir/probe-root"
probe_bin="$tmp_dir/probe-bin"
fallback_marker="$tmp_dir/ensure-postgres-called"
mkdir -p "$probe_root/scripts" "$probe_root/server" "$probe_bin"
cat > "$probe_bin/go" <<'EOF'
#!/usr/bin/env bash
write_probe_endpoint() {
  [ -z "${POSTGRES_PROBE_ENDPOINT_FILE:-}" ] \
    || printf '127.0.0.1:%s' "$POSTGRES_PORT" > "$POSTGRES_PROBE_ENDPOINT_FILE"
}
case "${EXPECTED_SYSTEM_ID:-}" in
  *[![:space:]]*) ;;
  *)
    echo "DATABASE_URL reached PostgreSQL, but this Compose project's identity could not be read" >&2
    exit 1
    ;;
esac
case "${POSTGRES_PROBE_RESULT:-failure}" in
  refused) write_probe_endpoint; printf %s "nothing-listening" ;;
  token-extra) write_probe_endpoint; printf 'nothing-listening\nunexpected-output' ;;
  ready)
    write_probe_endpoint
    printf 'ready\nPostgreSQL identity verified through DATABASE_URL.\n'
    ;;
  destroy-ready)
    write_probe_endpoint
    [ "${POSTGRES_PROBE_MODE:-ensure}" != destroy ] \
      || printf '%s\n' "$POSTGRES_DB" > "$POSTGRES_DROP_MARKER"
    printf 'dropped\nDropped database %s through verified DATABASE_URL.\n' "$POSTGRES_DB"
    ;;
  destroy-mismatch)
    write_probe_endpoint
    echo "DATABASE_URL reached 127.0.0.1:$POSTGRES_PORT, which is not this Compose project's PostgreSQL" >&2
    exit 1
    ;;
  destroy-failure)
    write_probe_endpoint
    echo "drop development database: forced test failure" >&2
    exit 1
    ;;
  real)
    cd "$POSTGRES_PROBE_SOURCE"
    exec "$REAL_GO" run ./cmd/postgres-probe
    ;;
  *) echo "pgx probe could not verify the listener" >&2; exit 1 ;;
esac
EOF
cat > "$probe_bin/docker" <<'EOF'
#!/usr/bin/env bash
case " $* " in
  *" compose -p "*" exec -T postgres "*)
    [ -z "${COMPOSE_IDENTITY_MARKER:-}" ] \
      || printf '%s\n' "$*" >> "$COMPOSE_IDENTITY_MARKER"
    printf '%s\n' test-system-identifier
    exit
    ;;
  *" compose -p "*" down --volumes "*)
    printf 'project=%s port=%s args=%s\n' \
      "${COMPOSE_PROJECT_NAME:-<unset>}" "${POSTGRES_PORT:-<unset>}" "$*" >> "$COMPOSE_DOWN_MARKER"
    [ "${FAIL_COMPOSE_DOWN:-0}" != 1 ] || exit 42
    exit 0
    ;;
esac
exit 1
EOF
cat > "$probe_root/scripts/ensure-postgres.sh" <<EOF
#!/usr/bin/env bash
printf '%s:%s\n' "\$COMPOSE_PROJECT_NAME" "\$POSTGRES_PORT" > "$fallback_marker"
EOF
chmod +x "$probe_bin/go" "$probe_bin/docker" "$probe_root/scripts/ensure-postgres.sh"

# Give the fixture every utility exercised by the registry paths while keeping
# host PostgreSQL clients out of PATH so the pgx-only contract is non-vacuous.
for probe_tool in awk basename bash cat chmod curl date dirname env grep head kill lsof \
  mkdir mktemp mv node nohup ps rm sed seq sleep tr uname wc; do
  probe_tool_path="$(command -v "$probe_tool" || true)"
  [ -z "$probe_tool_path" ] || ln -s "$probe_tool_path" "$probe_bin/$probe_tool"
done
probe_path="$probe_bin"
if PATH="$probe_path" command -v psql >/dev/null 2>&1 \
  || PATH="$probe_path" command -v pg_isready >/dev/null 2>&1; then
  fail "database guard test PATH unexpectedly contains psql or pg_isready"
fi

# A legacy/shared manifest has no owned Compose project to identify. Do not
# fall back to ambient Compose selection: an empty identifier makes the probe
# refuse any PostgreSQL server it reaches before a CREATE or DROP operation.
empty_identity_marker="$tmp_dir/empty-compose-identity-marker"
empty_identity="$(PATH="$probe_path" COMPOSE_IDENTITY_MARKER="$empty_identity_marker" \
  COMPOSE_PROJECT_NAME= bash -c 'source "$1"; compose_postgres_system_id' _ \
  "$root_dir/scripts/dev-env.sh")"
[ -z "$empty_identity" ] || fail "an empty Compose project produced a system identifier"
[ ! -e "$empty_identity_marker" ] || fail "an empty Compose project invoked Docker"

# The shared human path must pin Compose's normalized checkout-directory name
# in both the registry and checkout env. The first cycle may start PostgreSQL
# after a refused connection; the second must verify that same running stack.
human_root="$tmp_dir/Human.Checkout"
human_env="$human_root/.env"
human_identity_marker="$tmp_dir/human-compose-identity-marker"
mkdir -p "$human_root/scripts" "$human_root/server"
cp "$root_dir/scripts/local-env.sh" "$human_root/scripts/local-env.sh"
cp "$probe_root/scripts/ensure-postgres.sh" "$human_root/scripts/ensure-postgres.sh"
cat > "$human_env" <<'EOF'
POSTGRES_DB=multica_human
POSTGRES_USER=multica
POSTGRES_PASSWORD=multica
POSTGRES_PORT=5432
COMPOSE_PROJECT_NAME=ambient-wrong
DATABASE_URL=postgres://multica:multica@localhost:5432/multica_human?sslmode=disable
PORT=18920
FRONTEND_PORT=13840
EOF
rm -f "$fallback_marker"
PATH="$probe_path" COMPOSE_IDENTITY_MARKER="$human_identity_marker" \
  ROOT_SCRIPT="$root_dir/scripts/dev-env.sh" HUMAN_ROOT="$human_root" \
  bash -c '
    source "$ROOT_SCRIPT"
    REPO_ROOT="$HUMAN_ROOT"
    cmd_gc() { :; }
    migrate_database() { :; }
    start_api() { :; }
    print_handoff() { :; }
    port_free() { return 0; }
    export POSTGRES_PROBE_RESULT=refused
    cmd_up --name human-shared --components api
    export POSTGRES_PROBE_RESULT=ready
    cmd_up --name human-shared --components api
  ' > "$out" 2>&1 || {
    sed 's/^/  /' "$out" >&2
    fail "two consecutive human-owned up cycles did not succeed"
  }
human_manifest="$MULTICA_DEV_HOME/envs/human-shared/manifest.env"
grep -Fxq 'COMPOSE_PROJECT_NAME=humancheckout' "$human_manifest" \
  || fail "human manifest did not pin Compose's normalized checkout identity"
grep -Fxq 'COMPOSE_PROJECT_NAME=humancheckout' "$human_env" \
  || fail "human env file did not carry the pinned Compose identity"
[ "$(wc -l < "$fallback_marker")" -eq 1 ] \
  || fail "the second human up cycle started Compose instead of verifying it"
[ "$(grep -Fc 'compose -p humancheckout exec -T postgres psql' "$human_identity_marker")" -eq 2 ] \
  || fail "human up cycles did not read the same explicit Compose identity"

# Human manifests written before the identity field was pinned derive the same
# project from their recorded checkout path instead of forwarding an empty ID.
write_manifest "legacy-human-914" "$human_root" 914 human
legacy_human_project="$(bash -c '
  source "$1"
  load_manifest legacy-human-914
  printf %s "$COMPOSE_PROJECT_NAME"
' _ "$root_dir/scripts/dev-env.sh")"
[ "$legacy_human_project" = humancheckout ] \
  || fail "legacy human manifest did not derive its Compose identity"
rm -rf "$MULTICA_DEV_HOME/envs/legacy-human-914"
rm -f "$fallback_marker"
status=0
PATH="$probe_path" POSTGRES_PROBE_RESULT=failure REPO_ROOT="$probe_root" \
  DATABASE_URL='postgres://multica:wrong@127.0.0.1:25432/multica?sslmode=disable' \
  POSTGRES_DB=multica POSTGRES_PORT=25432 COMPOSE_PROJECT_NAME=multica_ephemeral_321 \
  bash -c 'source "$1"; REPO_ROOT="$2"; ensure_database' _ \
    "$root_dir/scripts/dev-env.sh" "$probe_root" > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "unverifiable pgx result reached the Compose fallback"
[ ! -e "$fallback_marker" ] || fail "unverifiable pgx result started PostgreSQL"
require_contains "$out" "pgx probe could not verify the listener"

PATH="$probe_path" POSTGRES_PROBE_RESULT=refused REPO_ROOT="$probe_root" \
  DATABASE_URL='postgres://multica:multica@127.0.0.1:25432/multica?sslmode=disable' \
  POSTGRES_DB=multica POSTGRES_PORT=25432 COMPOSE_PROJECT_NAME=multica_ephemeral_321 ENV_FILE=.env \
  bash -c 'source "$1"; REPO_ROOT="$2"; ensure_database' _ \
    "$root_dir/scripts/dev-env.sh" "$probe_root" > "$out" 2>&1 \
  || fail "a positively refused connection did not reach the Compose fallback"
[ "$(cat "$fallback_marker")" = 'multica_ephemeral_321:25432' ] \
  || fail "Compose fallback did not receive the upstream project and port"

rm -f "$fallback_marker"
status=0
real_go="$(command -v go)"
query_port="$(node -e '
  const server = require("net").createServer();
  server.listen(0, "127.0.0.1", () => {
    process.stdout.write(String(server.address().port));
    server.close();
  });
')"
PATH="$probe_path" POSTGRES_PROBE_RESULT=real REAL_GO="$real_go" \
  POSTGRES_PROBE_SOURCE="$root_dir/server" REPO_ROOT="$probe_root" \
  DATABASE_URL="postgres://multica:multica@127.0.0.1:25432/multica?sslmode=disable&port=$query_port" \
  POSTGRES_DB=multica POSTGRES_PORT=25432 COMPOSE_PROJECT_NAME=multica_ephemeral_321 ENV_FILE=.env \
  bash -c 'source "$1"; REPO_ROOT="$2"; ensure_database' _ \
    "$root_dir/scripts/dev-env.sh" "$probe_root" > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "a pgx query-port override reached the Compose fallback"
[ ! -e "$fallback_marker" ] || fail "a refused connection on a different effective endpoint started PostgreSQL"

rm -f "$fallback_marker"
status=0
PATH="$probe_path" POSTGRES_PROBE_RESULT=token-extra REPO_ROOT="$probe_root" \
  DATABASE_URL='postgres://multica:multica@127.0.0.1:25432/multica?sslmode=disable' \
  POSTGRES_DB=multica POSTGRES_PORT=25432 COMPOSE_PROJECT_NAME=multica_ephemeral_321 ENV_FILE=.env \
  bash -c 'source "$1"; REPO_ROOT="$2"; ensure_database' _ \
    "$root_dir/scripts/dev-env.sh" "$probe_root" > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "a multi-line nothing-listening result reached the Compose fallback"
[ ! -e "$fallback_marker" ] || fail "trailing probe output started PostgreSQL"

# ensure-postgres sources the checkout env file in a subprocess. It must retain
# the explicit Make override instead of silently restoring the file's port.
ensure_bin="$tmp_dir/ensure-bin"
ensure_env="$tmp_dir/ensure.env"
ensure_port_marker="$tmp_dir/ensure-port"
mkdir -p "$ensure_bin"
cat > "$ensure_env" <<'EOF'
POSTGRES_DB=multica
POSTGRES_USER=multica
POSTGRES_PASSWORD=multica
POSTGRES_PORT=5432
COMPOSE_PROJECT_NAME=multica_test
DATABASE_URL=postgres://multica:multica@localhost:5432/multica?sslmode=disable
EOF
cat > "$ensure_bin/docker" <<EOF
#!/usr/bin/env bash
case " \$* " in
  *" compose up "*) printf '%s\n' "\$POSTGRES_PORT" > "$ensure_port_marker" ;;
  *" psql "*) printf '1\n' ;;
esac
EOF
chmod +x "$ensure_bin/docker"
PATH="$ensure_bin:$PATH" MULTICA_POSTGRES_PORT_OVERRIDE=25432 \
  bash "$root_dir/scripts/ensure-postgres.sh" "$ensure_env" > "$out" 2>&1 \
  || fail "ensure-postgres rejected an explicit PostgreSQL port"
[ "$(cat "$ensure_port_marker")" = 25432 ] \
  || fail "ensure-postgres restored the env-file PostgreSQL port"

# A port without its Compose identity is the incident shape. Refuse before any
# Docker command can fall through to Compose's default `multica` project.
missing_project_env="$tmp_dir/missing-project.env"
cat > "$missing_project_env" <<'EOF'
POSTGRES_DB=multica
POSTGRES_USER=multica
POSTGRES_PASSWORD=multica
POSTGRES_PORT=25432
DATABASE_URL=postgres://multica:multica@localhost:25432/multica?sslmode=disable
EOF
rm -f "$ensure_port_marker"
status=0
PATH="$ensure_bin:$PATH" \
  bash "$root_dir/scripts/ensure-postgres.sh" "$missing_project_env" > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "ensure-postgres defaulted a port-bearing env file to project multica"
[ ! -e "$ensure_port_marker" ] || fail "ensure-postgres invoked Docker without a Compose project"
require_contains "$out" "POSTGRES_PORT but no COMPOSE_PROJECT_NAME"
# ---------------------------------------------------------------------------
# A registered environment is visible to both renderings, and the JSON one
# parses — agents read it, so a stray log line in it is a broken contract.
# ---------------------------------------------------------------------------
write_manifest "probe-901" "$tmp_dir/checkout" 901
mkdir -p "$tmp_dir/checkout"

dev_env list > "$out" 2>&1 || fail "list must succeed with one environment"
require_contains "$out" "probe-901"
require_contains "$out" "18981"

dev_env status probe-901 --json > "$out" 2>&1 || fail "status --json must succeed"
node -e '
  const fs = require("fs");
  const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  if (payload.name !== "probe-901") throw new Error("name = " + payload.name);
  if (payload.backend_port !== 18981) throw new Error("backend_port = " + payload.backend_port);
  for (const key of ["api", "web", "daemon", "desktop"]) {
    if (!payload.components[key]) throw new Error("missing component " + key);
    if (payload.components[key].state !== "stopped") {
      throw new Error(key + " state = " + payload.components[key].state);
    }
  }
' "$out" || fail "status --json is not machine-readable"

# ---------------------------------------------------------------------------
# Stopping an environment that is not running is a no-op that SUCCEEDS.
#
# This is the regression that made `make down` exit 1 after reporting success:
# on bash 3.2 a command substitution whose function ends in a failing command
# aborts the whole script under `set -e`, and "no process is listening on this
# port" is that function's normal answer.
# ---------------------------------------------------------------------------
status=0
dev_env down probe-901 --components api,web > "$out" 2>&1 || status=$?
if [ "$status" -ne 0 ]; then
  echo "Observed:" >&2
  sed 's/^/  /' "$out" >&2
  fail "down on a stopped environment exited $status, want 0"
fi
require_contains "$out" "stopped"

# Commands launched through env-exec must not inherit the daemon-task identity
# hints that make human/profile CLI commands reject --profile.
write_manifest "clean-env-903" "$root_dir" 903
MULTICA_TASK_CONFIG_ROOT=/task/config \
MULTICA_TASK_WORKSPACES_ROOT=/task/workspaces \
MULTICA_WORKSPACES_ROOT=/owner/workspaces \
  dev_env exec clean-env-903 -- sh -c '
    test -z "${MULTICA_TASK_CONFIG_ROOT:-}" &&
    test -z "${MULTICA_TASK_WORKSPACES_ROOT:-}" &&
    test "$MULTICA_WORKSPACES_ROOT" = "$1"
  ' _ "$MULTICA_DEV_WORKSPACES_PARENT/multica_workspaces_dev-dev-env-test-903" \
  > "$out" 2>&1 || fail "env-exec leaked daemon task identity or owner workspaces root"

# A health response without process identity is never proof that the process is
# this checkout's freshly launched API.
if bash -c 'source "$1"; api_started_after '\''{"status":"ok"}'\'' 1' _ "$root_dir/scripts/dev-env.sh"; then
  fail "legacy /health without started_at was accepted as current"
fi

# Turbo starts its package task in a nested process group. That listener is
# still owned by the launcher's live process tree and must be accepted, then
# pinned by PID for safe teardown.
descendant_pid_file="$tmp_dir/descendant-pid"
sh -c 'setsid sh -c '\''printf %s "$$" > "$1"; sleep 30'\'' _ "$1" & wait' _ "$descendant_pid_file" &
ancestor_pid=$!
for _ in $(seq 1 50); do
  [ -s "$descendant_pid_file" ] && break
  sleep 0.02
done
[ -s "$descendant_pid_file" ] || fail "nested process-group fixture did not start"
descendant_pid="$(cat "$descendant_pid_file")"
if ! bash -c 'source "$1"; process_descends_from "$2" "$3"' _ \
  "$root_dir/scripts/dev-env.sh" "$descendant_pid" "$ancestor_pid"; then
  kill -TERM -"$descendant_pid" "$ancestor_pid" 2>/dev/null || true
  fail "nested process group was not recognized as launcher-owned"
fi
kill -TERM -"$descendant_pid" "$ancestor_pid" 2>/dev/null || true
wait "$ancestor_pid" 2>/dev/null || true

# Some Linux lsof builds cannot enumerate an IPv6 wildcard listener even
# though ss reports its owning PID. Listener identity must retain that fallback
# or a healthy Next.js server is rejected as foreign.
listener_bin="$tmp_dir/listener-bin"
mkdir -p "$listener_bin"
cat > "$listener_bin/lsof" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat > "$listener_bin/ss" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' 'LISTEN 0 511 *:13691 *:* users:(("next-server",pid=4242,fd=22))'
EOF
chmod +x "$listener_bin/lsof" "$listener_bin/ss"
listener_pid="$(PATH="$listener_bin:$PATH" bash -c 'source "$1"; port_listener_pid 13691' _ \
  "$root_dir/scripts/dev-env.sh")"
[ "$listener_pid" = 4242 ] || fail "ss listener fallback returned $listener_pid, want 4242"

# ---------------------------------------------------------------------------
# Unknown names and components fail loudly instead of doing something else.
# ---------------------------------------------------------------------------
status=0
dev_env status no-such-env > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "status on an unknown environment must fail"
require_contains "$out" "Unknown environment"

status=0
dev_env up --components nope > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "up with an unknown component must fail"
require_contains "$out" "Unknown component"

# ---------------------------------------------------------------------------
# gc reports what it would collect and touches nothing in --dry-run. An
# environment whose checkout is gone has no owner left to stop it, which is how
# 152 databases accumulated with nothing on the machine able to list them.
# ---------------------------------------------------------------------------
write_manifest "orphan-902" "$tmp_dir/deleted-checkout" 902

dev_env gc --dry-run > "$out" 2>&1 || fail "gc --dry-run must succeed"
require_contains "$out" "orphan-902 would be collected"
if grep -Fq "probe-901 would be collected" "$out"; then
  fail "gc must not collect an environment whose directory still exists"
fi
[ -f "$MULTICA_DEV_HOME/envs/orphan-902/manifest.env" ] || fail "gc --dry-run deleted a manifest"

# Destroy uses the same repo-owned identity probe as create, including when no
# host PostgreSQL clients are installed. A mismatch must name the reached and
# expected endpoints, retain every cleanup recipe, and keep DROP unreachable.
destroy_drop_marker="$tmp_dir/destroy-drop-marker"
compose_down_marker="$tmp_dir/compose-down-marker"
compose_identity_marker="$tmp_dir/compose-identity-marker"
write_manifest "destroy-mismatch-905" "$root_dir" 905 agent multica_destroy-mismatch-905
mkdir -p "$MULTICA_DEV_PROFILES_HOME/dev-dev-env-test-905"
mkdir -p "$MULTICA_DEV_WORKSPACES_PARENT/multica_workspaces_dev-dev-env-test-905"
mkdir -p "$MULTICA_DEV_DESKTOP_APP_DATA/Multica Canary destroy-mismatch-905"
status=0
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-mismatch \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" \
  dev_env destroy destroy-mismatch-905 --yes > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "destroy accepted a PostgreSQL identity mismatch"
[ -f "$MULTICA_DEV_HOME/envs/destroy-mismatch-905/manifest.env" ] \
  || fail "destroy discarded the manifest after an identity mismatch"
[ -d "$MULTICA_DEV_PROFILES_HOME/dev-dev-env-test-905" ] \
  || fail "destroy removed the profile after an identity mismatch"
[ -d "$MULTICA_DEV_WORKSPACES_PARENT/multica_workspaces_dev-dev-env-test-905" ] \
  || fail "destroy removed daemon workspaces after an identity mismatch"
[ -d "$MULTICA_DEV_DESKTOP_APP_DATA/Multica Canary destroy-mismatch-905" ] \
  || fail "destroy removed Desktop data after an identity mismatch"
[ ! -e "$destroy_drop_marker" ] || fail "destroy dropped after an identity mismatch"
require_contains "$out" "reached 127.0.0.1:5432"
require_contains "$out" "expected this environment's Compose PostgreSQL at 127.0.0.1:5432"

# Automatic GC inherits the refusal and does not continue into broader cleanup.
write_manifest "gc-mismatch-906" "$tmp_dir/deleted-gc-checkout" 906 agent multica_gc-mismatch-906
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-mismatch \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" \
  dev_env gc --auto > "$out" 2>&1 || fail "automatic gc did not handle a refused destroy"
[ -f "$MULTICA_DEV_HOME/envs/gc-mismatch-906/manifest.env" ] \
  || fail "automatic gc discarded the manifest after an identity mismatch"
require_contains "$out" "automatic cleanup of gc-mismatch-906 failed; its manifest was kept for retry"

# Defense in depth: unattended GC only reports human-owned collectibles.
write_manifest "gc-human-907" "$tmp_dir/deleted-human-checkout" 907 human
dev_env gc --auto > "$out" 2>&1 \
  || fail "automatic gc failed while reporting a human-owned environment"
[ -f "$MULTICA_DEV_HOME/envs/gc-human-907/manifest.env" ] \
  || fail "automatic gc destroyed a human-owned environment"
require_contains "$out" "automatic cleanup skipped human-owned environment gc-human-907"

# A human-owned environment records the shared Compose identity so its database
# target can be verified, but it never owns that project's containers, network,
# or volumes. Exercise the standing-halt project-name shapes through destroy and
# assert against the Docker shim's recorded teardown invocations.
protected_human_projects=(multica parlour-dev footplate-prod-db blend-studio-db)
unsafe_human_releases=()
protected_offset=915
for protected_project in "${protected_human_projects[@]}"; do
  protected_checkout="$tmp_dir/protected-checkouts/$protected_project"
  protected_name="human-destroy-$protected_offset"
  mkdir -p "$protected_checkout"
  write_manifest \
    "$protected_name" "$protected_checkout" "$protected_offset" human "$protected_project"
  : > "$compose_down_marker"
  PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready \
    POSTGRES_DROP_MARKER="$destroy_drop_marker" COMPOSE_DOWN_MARKER="$compose_down_marker" \
    dev_env destroy "$protected_name" --yes > "$out" 2>&1 \
    || fail "human-owned destroy failed while skipping shared project $protected_project"
  if [ -s "$compose_down_marker" ]; then
    unsafe_human_releases+=("$protected_project")
  fi
  protected_offset=$((protected_offset + 1))
done
if [ "${#unsafe_human_releases[@]}" -ne 0 ]; then
  fail "human-owned destroy invoked Compose teardown for: ${unsafe_human_releases[*]}"
fi

# The ownership gate must not turn allocator-owned teardown into a no-op. An
# isolated environment still releases only the exact project derived from its
# allocated environment name.
isolated_owner_name="owner-gate-919"
isolated_owner_project="multica_$isolated_owner_name"
write_manifest \
  "$isolated_owner_name" "$root_dir" 919 agent "$isolated_owner_project"
: > "$compose_down_marker"
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" COMPOSE_DOWN_MARKER="$compose_down_marker" \
  dev_env destroy "$isolated_owner_name" --yes > "$out" 2>&1 \
  || fail "allocator-owned destroy did not release its isolated Compose project"
require_contains "$compose_down_marker" \
  "compose -p $isolated_owner_project down --volumes"
[ "$(wc -l < "$compose_down_marker")" -eq 1 ] \
  || fail "allocator-owned destroy invoked Docker more than once"

# A positively matched target still drops and releases the manifest without
# psql or pg_isready; the pgx probe owns the guarded DROP operation itself.
write_manifest "destroy-ready-908" "$root_dir" 908 agent multica_destroy-ready-908
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" COMPOSE_DOWN_MARKER="$compose_down_marker" \
  dev_env destroy destroy-ready-908 --yes > "$out" 2>&1 \
  || fail "destroy rejected a positively matched PostgreSQL target"
[ "$(cat "$destroy_drop_marker")" = multica_dev_env_test_908 ] \
  || fail "the verified pgx path did not drop the expected database"
[ ! -e "$MULTICA_DEV_HOME/envs/destroy-ready-908/manifest.env" ] \
  || fail "successful destroy retained its manifest"
require_contains "$out" "Dropped database multica_dev_env_test_908 through verified DATABASE_URL."

# An isolated environment owns its explicitly recorded Compose project. A
# successful destroy must release that project's container, network and named
# volumes before discarding the only manifest that names the target.
write_manifest "destroy-compose-909" "$root_dir" 909 agent multica_destroy-compose-909
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" COMPOSE_DOWN_MARKER="$compose_down_marker" \
  COMPOSE_IDENTITY_MARKER="$compose_identity_marker" \
  dev_env destroy destroy-compose-909 --yes > "$out" 2>&1 \
  || fail "destroy did not release its isolated Compose project"
require_contains "$compose_identity_marker" \
  "compose -p multica_destroy-compose-909 exec -T postgres psql"
require_contains "$compose_down_marker" "compose -p multica_destroy-compose-909 down --volumes"
[ ! -e "$MULTICA_DEV_HOME/envs/destroy-compose-909/manifest.env" ] \
  || fail "destroy retained the manifest after Compose teardown succeeded"
require_contains "$out" "released Compose project multica_destroy-compose-909"

# Compose teardown is part of destroy's success contract. If it fails, report
# failure and retain the manifest so the same explicit project can be retried.
write_manifest "destroy-compose-fails-910" "$root_dir" 910 agent multica_destroy-compose-fails-910
status=0
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready FAIL_COMPOSE_DOWN=1 \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" COMPOSE_DOWN_MARKER="$compose_down_marker" \
  dev_env destroy destroy-compose-fails-910 --yes > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "destroy succeeded after Compose teardown failed"
[ -f "$MULTICA_DEV_HOME/envs/destroy-compose-fails-910/manifest.env" ] \
  || fail "destroy discarded the manifest after Compose teardown failed"
require_contains "$out" "failed to release Compose project multica_destroy-compose-fails-910"
require_contains "$out" "manifest and other resources were kept"
if grep -Fq "destroy-compose-fails-910 destroyed" "$out"; then
  fail "destroy reported success after Compose teardown failed"
fi

# Automatic GC must route through the same release rather than consuming the
# manifest while leaving the isolated Compose project behind.
write_manifest "gc-compose-911" "$tmp_dir/deleted-gc-compose-checkout" 911 agent multica_gc-compose-911
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" COMPOSE_DOWN_MARKER="$compose_down_marker" \
  dev_env gc --auto > "$out" 2>&1 || fail "automatic gc did not release its isolated Compose project"
require_contains "$compose_down_marker" "compose -p multica_gc-compose-911 down --volumes"
[ ! -e "$MULTICA_DEV_HOME/envs/gc-compose-911/manifest.env" ] \
  || fail "automatic gc retained the manifest after Compose teardown succeeded"

# A failed database drop keeps the manifest and slot so cleanup can be retried;
# destroy must never print success and forget the only deletion recipe.
write_manifest "drop-fails-904" "$root_dir" 904 agent multica_drop-fails-904
status=0
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-failure \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" \
  dev_env destroy drop-fails-904 --yes > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "destroy succeeded after DROP DATABASE failed"
[ -f "$MULTICA_DEV_HOME/envs/drop-fails-904/manifest.env" ] \
  || fail "destroy discarded the manifest after DROP DATABASE failed"
require_contains "$out" "manifest and other resources were kept"
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" COMPOSE_DOWN_MARKER="$compose_down_marker" \
  dev_env destroy drop-fails-904 --yes > "$out" 2>&1 \
  || fail "retrying destroy after database recovery failed"

# Isolated PostgreSQL teardown uses the exact manifest values, even when the
# invoking shell carries conflicting values from another environment.
: > "$compose_down_marker"
write_manifest "compose-destroy-912" "$root_dir" 912 agent multica_compose-destroy-912 26555
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" COMPOSE_DOWN_MARKER="$compose_down_marker" \
  POSTGRES_PORT=29999 COMPOSE_PROJECT_NAME=multica_wrong-project \
  dev_env destroy compose-destroy-912 --yes > "$out" 2>&1 \
  || fail "isolated Compose destroy must succeed"
require_contains "$compose_down_marker" \
  "project=multica_compose-destroy-912 port=26555 args=compose -p multica_compose-destroy-912 down --volumes"
[ ! -d "$MULTICA_DEV_HOME/envs/compose-destroy-912" ] \
  || fail "successful Compose teardown retained the manifest and slot"

# A failed Compose teardown preserves its exit status and every registry value
# needed to retry the exact same project cleanup.
: > "$compose_down_marker"
write_manifest "compose-fails-913" "$root_dir" 913 agent multica_compose-fails-913 26556
status=0
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready FAIL_COMPOSE_DOWN=1 \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" COMPOSE_DOWN_MARKER="$compose_down_marker" \
  POSTGRES_PORT=29999 COMPOSE_PROJECT_NAME=multica_wrong-project \
  dev_env destroy compose-fails-913 --yes > "$out" 2>&1 || status=$?
[ "$status" -eq 42 ] || fail "failed Compose teardown exited $status, want 42"
[ -f "$MULTICA_DEV_HOME/envs/compose-fails-913/manifest.env" ] \
  || fail "failed Compose teardown discarded the manifest and slot"
if ! bash -c 'source "$1"; offset_registered "$2"' _ \
  "$root_dir/scripts/dev-env.sh" 913; then
  fail "failed Compose teardown released its allocator slot"
fi
require_contains "$compose_down_marker" \
  "project=multica_compose-fails-913 port=26556 args=compose -p multica_compose-fails-913 down --volumes"
require_contains "$out" "failed to release Compose project multica_compose-fails-913 (exit 42)"
require_contains "$out" "manifest and other resources were kept"
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready \
  POSTGRES_DROP_MARKER="$destroy_drop_marker" COMPOSE_DOWN_MARKER="$compose_down_marker" \
  dev_env destroy compose-fails-913 --yes > "$out" 2>&1 \
  || fail "retrying destroy after Compose recovery failed"

# ---------------------------------------------------------------------------
# A legacy manifest without a recorded Compose project cannot supply the
# production probe's mandatory identity. Refuse before running the probe or
# dropping anything, and retain the manifest as the only cleanup recipe.
# ---------------------------------------------------------------------------
no_project_drop_marker="$tmp_dir/no-project-drop-marker"
status=0
PATH="$probe_path" POSTGRES_PROBE_RESULT=destroy-ready \
  POSTGRES_DROP_MARKER="$no_project_drop_marker" \
  dev_env destroy probe-901 --yes > "$out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail "destroy accepted a manifest with no Compose identity"
[ ! -e "$no_project_drop_marker" ] || fail "destroy probed or dropped with no Compose identity"
[ -f "$MULTICA_DEV_HOME/envs/probe-901/manifest.env" ] \
  || fail "destroy discarded the no-project manifest"
require_contains "$out" "no Compose project recorded; refusing to probe or drop"

dev_env list > "$out" 2>&1 || fail "list must succeed after refused destroy"
require_contains "$out" "probe-901"

# Declining the confirmation is a successful no-op, not a failure.
write_manifest "orphan-902" "$tmp_dir/deleted-checkout" 902
printf 'n\n' | dev_env destroy orphan-902 > "$out" 2>&1 || fail "declining destroy must exit 0"
require_contains "$out" "Cancelled."
[ -d "$MULTICA_DEV_HOME/envs/orphan-902" ] || fail "declined destroy removed the environment anyway"

echo "✓ dev-env.sh registry behaviour verified"
