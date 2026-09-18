.PHONY: build web run test lint fmt generate tidy docker

build: web
	go build -trimpath -o bin/kabanbot ./cmd/kabanbot

web:
	cd web/miniapp && npm ci && npm run build

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
