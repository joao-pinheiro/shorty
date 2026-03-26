.PHONY: dev-backend dev-frontend build-backend build-frontend copy-frontend build \
        test-backend test-frontend test lint migrate \
        docker-build docker-up docker-down docker-logs docker-clean

# Development
dev-backend:
	cd backend && go run ./cmd/shorty

dev-frontend:
	cd frontend && npm run dev

# Build frontend
build-frontend:
	cd frontend && npm ci && npm run build

# Copy frontend dist into Go embed location
copy-frontend: build-frontend
	rm -rf backend/cmd/shorty/dist
	cp -r frontend/dist backend/cmd/shorty/dist

# Build backend (requires frontend to be built and copied first)
build-backend: copy-frontend
	cd backend && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../bin/shorty ./cmd/shorty

# Combined build
build: build-backend

# Testing
test-backend:
	cd backend && go test ./... -race

test-frontend:
	cd frontend && npm test

test: test-backend test-frontend

# Linting
lint:
	cd backend && go vet ./...

# Migration
migrate:
	cd backend && go run ./cmd/shorty -migrate

# Docker
docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

docker-clean:
	docker compose down -v
