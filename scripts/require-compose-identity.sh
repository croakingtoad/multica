#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"

# A missing file carries neither half of the isolation identity. Callers that
# require an env file own that separate error; self-hosting may create one.
[ -f "$ENV_FILE" ] || exit 0

env_file_has_postgres_port=0
env_file_has_compose_project=0
grep -Eq '^[[:space:]]*(export[[:space:]]+)?POSTGRES_PORT[[:space:]]*=' "$ENV_FILE" \
  && env_file_has_postgres_port=1
grep -Eq '^[[:space:]]*(export[[:space:]]+)?COMPOSE_PROJECT_NAME[[:space:]]*=' "$ENV_FILE" \
  && env_file_has_compose_project=1

[ "$env_file_has_postgres_port" = 1 ] || exit 0

refuse_missing_compose_project() {
  local reason=$1 quoted_env_file
  printf -v quoted_env_file '%q' "$ENV_FILE"
  printf 'Refusing Compose: %s carries POSTGRES_PORT but %s.\n' "$ENV_FILE" "$reason" >&2
  printf '%s\n' \
    "Remediation: run: read -r -p 'Compose project name: ' project && test -n \"\$project\" && printf '\\nCOMPOSE_PROJECT_NAME=%s\\n' \"\$project\" >> $quoted_env_file" >&2
  exit 1
}

[ "$env_file_has_compose_project" = 1 ] \
  || refuse_missing_compose_project 'no COMPOSE_PROJECT_NAME'

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

[ -n "${COMPOSE_PROJECT_NAME:-}" ] \
  || refuse_missing_compose_project 'COMPOSE_PROJECT_NAME is empty'
