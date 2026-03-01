# Stage 1: Build frontend
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build backend
FROM golang:1.24-alpine AS backend-builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend-builder /app/frontend/build ./frontend-dist
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /gatehouse ./cmd/gatehouse

# Stage 3: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=backend-builder /gatehouse /usr/local/bin/gatehouse
COPY --from=frontend-builder /app/frontend/build /app/frontend-dist
EXPOSE 8080
ENV FRONTEND_DIR=/app/frontend-dist
ENTRYPOINT ["gatehouse"]
