.PHONY: run-api run-worker build tidy test

# Load .env into the shell for local dev runs
include .env
export

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker

tidy:
	go mod tidy

test:
	go test ./...
