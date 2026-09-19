.PHONY: build run test lint fmt generate tidy docker

build:
	go build -trimpath -o bin/kabanbot ./cmd/kabanbot

run:
	go run ./cmd/kabanbot

test:
	go test -race ./...

lint:
	golangci-lint run ./...
	go fix -diff ./...

fmt:
	golangci-lint fmt ./...

generate:
	go tool sqlc generate

tidy:
	go mod tidy

docker:
	docker compose up -d --build
