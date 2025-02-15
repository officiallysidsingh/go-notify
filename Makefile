.PHONY: build run worker proto migrate-up migrate-down docker-up docker-down clean restart-rabbitmq test docker-build

# Variables
DB_URL := "postgres://notify:notify_pass@localhost:5432/go_notify?sslmode=disable"
MIGRATION_DIR := db/migrations
PROTO_PATH := api/proto
GENERATED_DIR := api/generated

# Directories
BIN_DIR := bin
CMD_DIR := ./cmd
SERVER_CMD := $(CMD_DIR)/server
WORKER_CMD := $(CMD_DIR)/worker

# Build both server and worker binaries
build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/server $(SERVER_CMD)/main.go
	go build -o $(BIN_DIR)/worker $(WORKER_CMD)/main.go

# Run gRPC server (using built binary)
run: build
	$(BIN_DIR)/server

# Run worker service (using built binary)
worker: build
	$(BIN_DIR)/worker

# Generate protobuf code
proto:
	protoc --go_out=$(GENERATED_DIR) --go-grpc_out=$(GENERATED_DIR) --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative --proto_path=$(PROTO_PATH) $(PROTO_PATH)/*.proto

# Build Docker images
docker-build:
	docker-compose -f ./deployments/docker-compose.yaml build

# Start Docker containers
docker-up: docker-build
	docker-compose -f ./deployments/docker-compose.yaml up -d

# Stop Docker containers
docker-down:
	docker-compose -f ./deployments/docker-compose.yaml down

# Restart RabbitMQ
restart-rabbitmq:
	docker restart rabbitmq

# Run tests
test:
	go test ./internal/... -v

# Database migrations
migrate-up:
	goose -dir $(MIGRATION_DIR) postgres $(DB_URL) up

migrate-down:
	goose -dir $(MIGRATION_DIR) postgres $(DB_URL) down

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR)

# Lint
lint:
	golangci-lint run
