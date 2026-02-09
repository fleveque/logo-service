.PHONY: build run test clean fmt vet lint cli docker-build docker-up docker-down import

# Build the server binary
build:
	go build -o bin/logo-service ./cmd/server
	go build -o bin/logo-cli ./cmd/cli

# Run the server (development)
run:
	go run ./cmd/server

# Run all tests
test:
	go test ./... -v

# Format all Go files
fmt:
	go fmt ./...

# Run Go vet (static analysis)
vet:
	go vet ./...

# Clean build artifacts
clean:
	rm -rf bin/

# Run the CLI
cli:
	go run ./cmd/cli $(ARGS)

# Trigger a GitHub logo import via CLI
import:
	go run ./cmd/cli import --source github

# Build Docker image
docker-build:
	docker build -t logo-service .

# Run with Docker Compose
docker-up:
	docker compose up -d

docker-down:
	docker compose down

# Deploy with Kamal
deploy:
	kamal deploy

# Kamal setup (first-time deploy)
deploy-setup:
	kamal setup
