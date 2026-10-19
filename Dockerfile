# Single-container supervisor image for Render / Fly.io / Container platforms.
# Builds the 6 Go microservices into compact binaries and runs them in a single container.
ARG GO_VERSION=1.25

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
ENV GOWORK=off CGO_ENABLED=0 GOTOOLCHAIN=auto

COPY pkg ./pkg
COPY gen ./gen
COPY services ./services

# Build all required microservices
RUN for svc in user product search order payment notification gateway; do \
      echo "Building service: $svc" && \
      cd /src/services/$svc && \
      go mod download && \
      go build -trimpath -ldflags="-s -w" -o /out/$svc ./cmd/server ; \
    done

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata bash curl postgresql-client

WORKDIR /app

# Copy binaries
COPY --from=build /out/* /app/
# Copy database migrations and seed script
COPY deployments/postgres/init-databases.sh /app/init-databases.sh
COPY services /app/services

# Runner script that handles DB init and runs all microservices concurrently
COPY deployments/render/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh /app/init-databases.sh

EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
