.PHONY: all build test test-race lint run-api run-worker run-frontend docker-up docker-down start start-ps

all: build test

build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker
	cd frontend && npm run build

test:
	go test -v ./tests/...

test-race:
	go test -v -race ./tests/...

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

run-frontend:
	cd frontend && npm run dev

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down -v

start:
	./start-all.sh

start-ps:
	powershell -ExecutionPolicy Bypass -File ./start-all.ps1
