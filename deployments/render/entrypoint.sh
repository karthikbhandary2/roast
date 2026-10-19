#!/usr/bin/env bash
set -e

echo "=== Roast Co. Backend Supervisor Starting ==="

# Wait for Postgres if DATABASE_URL or POSTGRES_URL is provided
DB_CONN="${DATABASE_URL:-$POSTGRES_URL}"
if [ -n "$DB_CONN" ]; then
  echo "Checking database connection..."
  # Run initial migrations if tables do not exist
  for svc in user product order payment notification; do
    if [ -f "/app/services/${svc}/migrations/001_init.sql" ]; then
      echo "Running migrations for ${svc}..."
      psql "$DB_CONN" -f "/app/services/${svc}/migrations/001_init.sql" || true
    fi
  done
fi

export USER_SERVICE_ADDR="127.0.0.1:9001"
export PRODUCT_SERVICE_ADDR="127.0.0.1:9002"
export SEARCH_SERVICE_ADDR="127.0.0.1:9003"
export ORDER_SERVICE_ADDR="127.0.0.1:9004"
export PAYMENT_SERVICE_ADDR="127.0.0.1:9005"
export NOTIFICATION_SERVICE_ADDR="127.0.0.1:9006"
export PAYMENT_HTTP_ADDR="http://127.0.0.1:9105"

echo "Starting microservices..."
/app/user &
/app/product &
/app/search &
/app/order &
/app/payment &
/app/notification &

# Sleep 2 seconds for internal gRPC servers to bind
sleep 2

echo "Starting Gateway on PORT ${PORT:-8080}..."
exec /app/gateway
