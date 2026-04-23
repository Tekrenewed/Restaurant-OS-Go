# Stage 1: Build the Go binary
FROM docker.io/library/golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go module files first for dependency caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /restaurant-os ./cmd/api

# Stage 2: Minimal production image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /restaurant-os .

# Copy SQL files for potential DB migrations
COPY --from=builder /app/internal/database/schema.sql ./migrations/schema.sql
COPY --from=builder /app/internal/database/seed.sql ./migrations/seed.sql

# Cloud Run sets PORT env var automatically
ENV PORT=8080

# SECURITY: Drop privileges from root to the unprivileged nobody user
USER nobody:nobody

EXPOSE 8080

CMD ["./restaurant-os"]
