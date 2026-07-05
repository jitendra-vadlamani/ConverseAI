.PHONY: build-client build-server build-all run clean dev-client dev-server docker-up docker-down pg-up infra-up infra-down app-up docker-logs docker-restart

BINARY_NAME=ai-chat-app

build-client:
	@echo "Building React client..."
	cd client && npm run build

build-server:
	@echo "Building Go server (embedding React assets)..."
	go build -o $(BINARY_NAME) ./cmd/app/main.go

build-all: build-client build-server

run: build-all
	./$(BINARY_NAME)

dev-client:
	cd client && npm run dev

dev-server:
	POSTGRES_URI="postgres://postgres:postgres@localhost:5432/converseai?sslmode=disable" go run ./cmd/app/main.go

docker-up:
	docker-compose up --build -d

docker-down:
	docker-compose down

pg-up:
	docker-compose up -d converseai-pg

infra-up: pg-up

infra-down:
	docker-compose stop converseai-pg
	docker-compose rm -f converseai-pg

app-up:
	docker-compose up -d converseai

docker-logs:
	docker-compose logs -f

docker-restart: docker-down docker-up

clean:
	rm -f $(BINARY_NAME)
	rm -f mcp-server
	rm -rf client/dist
