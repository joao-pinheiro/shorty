.PHONY: dev-backend dev-frontend migrate

dev-backend:
	cd backend && go run ./cmd/shorty

dev-frontend:
	cd frontend && npm run dev

migrate:
	cd backend && go run ./cmd/shorty -migrate
