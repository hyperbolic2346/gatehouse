.PHONY: dev-frontend dev-backend dev build docker-build clean

# Development
dev-frontend:
	cd frontend && npm run dev

dev-backend:
	cd backend && go run ./cmd/gatehouse

dev: ## Run both frontend and backend in development mode
	$(MAKE) dev-backend & $(MAKE) dev-frontend

# Build
build-frontend:
	cd frontend && npm ci && npm run build

build-backend:
	cd backend && CGO_ENABLED=1 go build -o gatehouse ./cmd/gatehouse

build: build-frontend build-backend

# Docker
docker-build:
	docker build -t gatehouse:latest .

# Clean
clean:
	rm -rf frontend/build frontend/.svelte-kit frontend/node_modules
	rm -f backend/gatehouse
