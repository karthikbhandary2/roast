# Recipes are prefixed with ">" instead of a tab (see .RECIPEPREFIX), so this
# file survives copy/paste from chat or an editor that converts tabs to spaces.
.RECIPEPREFIX := >

# Every Go module in the repo. Add each new service here.
MODULES := pkg gen/go services/gateway services/user services/product services/payment services/order services/search services/notification

.PHONY: help proto tidy build test up up-min down reset logs ps

help:
> @echo "proto   generate Go code from proto/ (needs buf)"
> @echo "tidy    go mod tidy in every module"
> @echo "build   go build every service module"
> @echo "test    go test every module"
> @echo "up      start everything with docker compose"
> @echo "up-min  start only what the User Service slice needs"
> @echo "down    stop containers (keeps data)"
> @echo "reset   stop containers AND delete data volumes (re-runs DB init)"
> @echo "logs    follow logs, e.g. make logs s=user-service"

proto:
> buf generate

tidy:
> @for m in $(MODULES); do echo "== tidy $$m"; (cd $$m && go mod tidy) || exit 1; done

build:
> @for m in services/gateway services/user services/product services/payment services/order services/search services/notification; do echo "== build $$m"; (cd $$m && go build ./...) || exit 1; done

test:
> @for m in $(MODULES); do echo "== test $$m"; (cd $$m && go test ./...) || exit 1; done

up:
> docker compose up -d --build

up-min:
> docker compose up -d --build postgres redis jaeger kafka user-service gateway

down:
> docker compose down

reset:
> docker compose down -v

logs:
> docker compose logs -f --tail=100 $(s)

ps:
> docker compose ps
