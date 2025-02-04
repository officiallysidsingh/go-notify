.PHONY: build run clean proto test restart-rabbitmq

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server/main.go

proto:
	protoc --go_out=api/generated --go-grpc_out=api/generated --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative --proto_path=api/proto api/proto/*.proto

docker-up:
	docker-compose -f ./deployments/docker-compose.yaml up -d

docker-down:
	docker-compose -f ./deployments/docker-compose.yaml down

restart-rabbitmq:
	docker restart rabbitmq

test:
	go test ./internal/... -v
