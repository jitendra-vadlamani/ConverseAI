.PHONY: build-client build-server build-all run clean dev-client dev-server docker-up docker-down db-up app-up docker-logs docker-restart

BINARY_NAME=ai-chat-app

build-client:
	@echo "Building React client..."
	cd client && npm run build

build-server:
	@echo "Building Go server (embedding React assets)..."
	go build -o $(BINARY_NAME) .

build-all: build-client build-server

run: build-all
	./$(BINARY_NAME)

dev-client:
	cd client && npm run dev

dev-server:
	go run .

docker-up:
	docker-compose up --build -d

docker-down:
	docker-compose down

db-up:
	docker-compose up -d converseai-db

app-up:
	docker-compose up -d converseai

docker-logs:
	docker-compose logs -f

docker-restart: docker-down docker-up

clean:
	rm -f $(BINARY_NAME)
	rm -rf client/dist
