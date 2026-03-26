.PHONY: dev-backend migrate

dev-backend:
	cd backend && go run ./cmd/shorty

migrate:
	cd backend && go run ./cmd/shorty -migrate
