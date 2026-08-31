# PostgreSQL Development Isolation Task Breakdown

**Goal:** Make development database startup fail closed without host PostgreSQL tools and give every ephemeral environment its own Compose project and published port.

**Approach:** Replace the host `psql` probe with a small pgx command that returns a dedicated result only when a TCP connection was positively refused; every other probe or identity failure aborts. Persist an ephemeral environment's Compose project and PostgreSQL port in its manifest, export them before database startup, and preserve an explicitly exported `POSTGRES_PORT` through Make's env-file include.

**Skills:** @go-dev, @task-breakdown

**Tech Details:** Bash 3.2-compatible shell, GNU Make, Go 1.26 module, pgx/v5, Docker Compose

---

### Task 1: Specify fail-closed shell and Make behavior

**Files:**
- Modify: `scripts/dev-env.test.sh`
- Modify: `scripts/makefile-build.test.sh`

**Step 1: Write failing tests**

Add shell cases that place fake `go` and `docker` commands on `PATH`, source `scripts/dev-env.sh`, and assert:

```bash
POSTGRES_PROBE_RESULT=unreachable ensure_database
test ! -e "$fallback_marker"

POSTGRES_PROBE_RESULT=refused ensure_database
grep -Fx "multica_ephemeral_321:25432" "$fallback_marker"
```

Add a Make evaluation asserting an exported shell value survives `.env.example`:

```bash
actual="$(POSTGRES_PORT=25753 make -s --eval 'print-postgres-port:;@echo $(POSTGRES_PORT)' print-postgres-port)"
[ "$actual" = 25753 ]
```

**Step 2: Run tests to verify they fail**

Run: `bash scripts/dev-env.test.sh && bash scripts/makefile-build.test.sh`

Expected: FAIL because the current shell path skips the probe without `psql`, does not export isolated Compose settings, and Make reports the env-file port.

**Step 3: Keep the harness isolated**

Use only throwaway directories and fake executables. Do not invoke Docker or bind a production/off-limits port from unit tests.

### Task 2: Add the pgx probe

**Files:**
- Create: `server/cmd/postgres-probe/main.go`
- Create: `server/cmd/postgres-probe/main_test.go`

**Step 1: Write classification tests**

Cover `ECONNREFUSED` as the sole start-safe result and verify authentication, timeout, malformed configuration, and arbitrary failures remain fatal:

```go
tests := []struct {
	name string
	err  error
	want connectState
}{
	{name: "connection refused", err: syscall.ECONNREFUSED, want: connectNothingListening},
	{name: "authentication", err: &pgconn.PgError{Code: "28P01"}, want: connectFailure},
	{name: "timeout", err: context.DeadlineExceeded, want: connectFailure},
}
```

**Step 2: Run the focused test to verify it fails**

Run: `cd server && go test ./cmd/postgres-probe`

Expected: FAIL because the command does not exist.

**Step 3: Implement the minimal command**

The command reads `DATABASE_URL`, `POSTGRES_DB`, and `EXPECTED_SYSTEM_ID`; connects with a three-second context; and:

```go
conn, err := pgx.Connect(ctx, databaseURL)
if errors.Is(err, syscall.ECONNREFUSED) {
	fmt.Fprintln(os.Stdout, "nothing-listening")
	return exitSuccess
}
if err != nil {
	return fail("database probe failed", err)
}

var systemID string
if err := conn.QueryRow(ctx,
	"SELECT system_identifier::text FROM pg_control_system()",
).Scan(&systemID); err != nil {
	return fail("read PostgreSQL system identifier", err)
}
if expectedSystemID == "" || systemID != expectedSystemID {
	return fail("database target is not this Compose project", nil)
}
```

After identity matches, query `pg_database` and execute a safely quoted `CREATE DATABASE` only when absent. Print no credentials.

**Step 4: Format and verify**

Run: `gofmt -w server/cmd/postgres-probe/*.go`

Run: `cd server && go test -race ./cmd/postgres-probe`

Expected: PASS.

### Task 3: Persist ephemeral Compose isolation and integrate the probe

**Files:**
- Modify: `scripts/dev-env.sh`
- Modify: `Makefile`
- Modify: `scripts/ensure-postgres.sh`

**Step 1: Add deterministic isolated settings**

Derive the database port from the already-exclusive environment offset, verify it is absent from `docker ps`, absent from `lsof`, and bindable on `127.0.0.1`, then persist both values:

```bash
POSTGRES_PORT="$(postgres_port_for_offset "$OFFSET")"
COMPOSE_PROJECT_NAME="multica_${NAME}"
export POSTGRES_PORT COMPOSE_PROJECT_NAME
```

Store `POSTGRES_PORT` and `COMPOSE_PROJECT_NAME` in `manifest.env`, and rewrite the ignored checkout env file so `DATABASE_URL` uses the same published port.

**Step 2: Replace the shell probe**

Read the current Compose container's system identifier through `docker compose exec`. Invoke `go run ./cmd/postgres-probe` from `server/`; only its exact `nothing-listening` result may call `scripts/ensure-postgres.sh`. Every error or unexpected result is surfaced and aborts.

**Step 3: Preserve an explicitly exported Make value**

Capture `POSTGRES_PORT` before `include $(ENV_FILE)`, restore it with `override` after the include, and keep command-line assignments highest priority.

**Step 4: Run local checks**

Run: `bash -n scripts/dev-env.sh scripts/dev-env.test.sh scripts/ensure-postgres.sh`

Run: `bash scripts/dev-env.test.sh`

Run: `bash scripts/makefile-build.test.sh`

Run: `cd server && go test -race ./cmd/postgres-probe`

Expected: all PASS.

### Task 4: Exercise the real safety boundary

**Files:**
- No tracked files

**Step 1: Snapshot protected containers**

Run `docker inspect` for `multica-postgres-1`, `parlour-pg`, `loco591-pg`, `footplate-prod-db`, and `blend-studio-db`, recording `StartedAt`, state, and live bindings without changing them.

**Step 2: Select and prove a free port**

Confirm the candidate does not appear in `docker ps` published bindings, then bind `127.0.0.1:<port>` with a short-lived Node listener and close it.

**Step 3: Verify the no-client path**

Run the real ephemeral startup with a `PATH` containing neither `psql` nor `pg_isready` against an authentication failure. Capture raw output proving the command aborts before any Compose start.

**Step 4: Verify isolated Compose startup**

Run `make up C=api ARGS='--ephemeral --name <unique-name>'` with the proven-free port. Capture raw `docker compose ls`, `docker ps`, and project-container output showing the isolated project and port while project `multica` remains unchanged.

**Step 5: Re-snapshot protected containers**

Repeat the read-only inspect and diff it against the initial snapshot. Do not clean up containers or volumes; report the isolated artifact for its owner to remove safely.

### Task 5: Review and commit

**Files:**
- Review all files above

**Step 1: Review the diff**

Run: `git diff --check && git diff -- scripts/dev-env.sh scripts/dev-env.test.sh scripts/makefile-build.test.sh Makefile server/cmd/postgres-probe docs/2026-08-31-postgres-isolation-tasks.md`

Expected: no whitespace errors; every fallback is guarded by the dedicated refused-connection status.

**Step 2: Commit atomically**

Run: `git add ... && git commit -m 'fix(dev-env): isolate ephemeral PostgreSQL startup'`

**Step 3: Record evidence**

Run: `git show --stat --oneline HEAD`

Expected: one local commit containing the probe, tests, environment wiring, and this breakdown.
