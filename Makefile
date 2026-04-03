.PHONY: build-client build-server build-all run clean dev-client dev-server docker-up docker-down db-up

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
	docker-compose up -d mongodb

clean:
	rm -f $(BINARY_NAME)
	rm -rf client/dist
