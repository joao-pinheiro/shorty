# ============================================================
# Stage 1: Build frontend
# ============================================================
FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

# ============================================================
# Stage 2: Build backend with embedded frontend
# ============================================================
FROM golang:1.24-alpine AS backend-builder

WORKDIR /app

COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download

COPY backend/ ./backend/

# Copy built frontend into the embed location
COPY --from=frontend-builder /app/frontend/dist ./backend/cmd/shorty/dist

# Build static binary
RUN cd backend && CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /shorty \
    ./cmd/shorty

# Create /data directory for the final stage
RUN mkdir -p /data

# ============================================================
# Stage 3: Minimal runtime
# ============================================================
FROM gcr.io/distroless/static-debian12

COPY --from=backend-builder /shorty /shorty
COPY --from=backend-builder --chown=65534:65534 /data /data

VOLUME ["/data"]
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/shorty"]
