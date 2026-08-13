#!/bin/sh
set -eu

if [ -z "${SYNCAM_POSTGRES_APP_PASSWORD:-}" ]; then
  echo "SYNCAM_POSTGRES_APP_PASSWORD is required" >&2
  exit 1
fi

psql --set=ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=app_password="$SYNCAM_POSTGRES_APP_PASSWORD" <<'EOSQL'
SELECT format(
  'CREATE ROLE syncam_app LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS',
  :'app_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'syncam_app')
\gexec
EOSQL
