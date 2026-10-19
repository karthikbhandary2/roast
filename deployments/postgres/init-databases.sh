#!/usr/bin/env bash
# Runs ONCE, the first time Postgres starts with an empty data volume (that is
# how the official image treats /docker-entrypoint-initdb.d). To re-run it:
#   make reset
set -e

# One database per service, matching the "each service owns its data" rule.
for svc in user product order payment notification; do
  echo "init: creating ${svc}_db if needed"
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres -tc \
    "SELECT 1 FROM pg_database WHERE datname = '${svc}_db'" | grep -q 1 || \
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
    -c "CREATE DATABASE ${svc}_db"
done

# Services whose schema is applied automatically. Add a name here as each
# service gets built (its migrations folder is already mounted at /migrations).
for svc in user product order payment notification; do
  echo "init: applying ${svc} migrations"
  psql --username "$POSTGRES_USER" --dbname "${svc}_db" \
    -f "/migrations/${svc}/001_init.sql" || true
done

# DEV ONLY: seed an admin so the seller-approval flow can be tried.
echo "init: seeding dev admin ${DEV_ADMIN_EMAIL}"
psql --username "$POSTGRES_USER" --dbname user_db \
  -v email="${DEV_ADMIN_EMAIL}" -v pw="${DEV_ADMIN_PASSWORD}" <<'EOSQL' || true
WITH u AS (
  INSERT INTO users (email, password_hash, display_name)
  VALUES (:'email', crypt(:'pw', gen_salt('bf', 12)), 'Dev Admin')
  ON CONFLICT (email) DO NOTHING
  RETURNING id
)
INSERT INTO user_roles (user_id, role)
SELECT u.id, r.role FROM u, (VALUES ('admin'::user_role), ('buyer'::user_role)) AS r(role)
ON CONFLICT DO NOTHING;
EOSQL
