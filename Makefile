.PHONY: build build-client build-server run dev-client dev-server test test-integration lint ci-services ci-services-down docker-up docker-down docker-logs backup restore eval clean

BINARY=converseai

build: build-client build-server

build-client:
	cd client && npm ci && npm run build

build-server:
	go build -o $(BINARY) .

run: build
	./$(BINARY) serve

dev-client:
	cd client && npm run dev

# Needs a .env with the secrets (see .env.example) and APP_ENV=development.
dev-server: client/dist/index.html
	go run . serve

client/dist/index.html:
	mkdir -p client/dist && echo '<!doctype html><title>run `make build-client`</title>' > $@

test: client/dist/index.html
	go test -race -count=1 ./internal/...

ci-services:
	docker compose -f docker-compose.ci.yml -p converseai-ci up -d

ci-services-down:
	docker compose -f docker-compose.ci.yml -p converseai-ci down -v

test-integration: client/dist/index.html ci-services
	go test -tags=integration -race -count=1 ./internal/e2e/

lint: client/dist/index.html
	test -z "$$(gofmt -l main.go internal)"
	go vet . ./internal/... ./cmd/...
	cd client && npm run lint

docker-up:
	docker compose up -d --build --wait

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

backup:
	scripts/backup.sh

restore:
	@test -n "$(FROM)" || (echo "usage: make restore FROM=backups/<timestamp>"; exit 1)
	scripts/restore.sh $(FROM)

# Runs the golden set against a running instance (needs real models).
eval:
	go run ./cmd/eval -base-url $${EVAL_BASE_URL:-http://localhost:8080} -golden evals/golden.jsonl -baseline evals/baseline.json -out evals/results

clean:
	rm -f $(BINARY)
	rm -rf client/dist
