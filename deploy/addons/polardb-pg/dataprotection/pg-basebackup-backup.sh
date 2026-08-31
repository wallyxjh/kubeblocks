set -euo pipefail

export PATH="${PATH}:${DP_DATASAFED_BIN_PATH}:${PG_BIN}"
export DATASAFED_BACKEND_BASE_PATH="${DP_BACKUP_BASE_PATH}"
export PGPASSWORD="${DP_DB_PASSWORD}"

trap handle_exit EXIT

if [[ ! -x "${PG_BIN}/pg_basebackup" || ! -x "${PG_BIN}/psql" ]]; then
  echo "the pinned runtime does not expose the expected PolarDB-PG client binaries at ${PG_BIN}" >&2
  exit 1
fi

if [[ "$("${PG_BIN}/psql" -U "${DP_DB_USER}" -h "${DP_DB_HOST}" -p "${DP_DB_PORT:-5432}" -d postgres -Atqc 'SHOW polar_deploy_mode;')" != "OPEN_SOURCE" ]]; then
  echo "refusing to use this action set against a non-PolarDB-PG endpoint" >&2
  exit 1
fi

start_time="$(get_current_time)"
# PostgreSQL 17 rejects -X stream when tar output is written to stdout. Fetch
# keeps the required WAL files in the tar stream and is supported by the
# official PolarDB-PG client used by this action set.
"${PG_BIN}/pg_basebackup" \
  -Ft -Pv -c fast -X fetch -D - \
  -h "${DP_DB_HOST}" -p "${DP_DB_PORT:-5432}" -U "${DP_DB_USER}" \
  | datasafed push -z zstd-fastest - "/${DP_BACKUP_NAME}.tar.zst"
stat_and_save_backup_info "${start_time}"
