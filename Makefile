.PHONY: build run test clean fmt vet lint

# Build the server binary
build:
	go build -o bin/logo-service ./cmd/server

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

# Build Docker image
docker-build:
	docker build -t logo-service .

# Run with Docker Compose
docker-up:
	docker compose up -d

docker-down:
	docker compose down
