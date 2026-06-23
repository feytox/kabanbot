# Build stage
FROM golang:1.26.4-alpine AS builder

WORKDIR /app

# Copy dependency files first to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy application source code
COPY . .

# Build a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o kabanbot main.go

# Final slim execution stage
FROM alpine:latest

# Install ca-certificates for secure HTTPS requests to Telegram and LLM APIs
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy static binary from the builder stage
COPY --from=builder /app/kabanbot /app/kabanbot

# Execute the application
ENTRYPOINT ["/app/kabanbot"]
