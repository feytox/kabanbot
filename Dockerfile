# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kabanbot ./cmd/kabanbot

# Runs as root: ./data is bind-mounted, and Docker creates it root-owned on a clean start
# (as are databases left by the Python version).
FROM gcr.io/distroless/static-debian13
WORKDIR /app
COPY --from=build /out/kabanbot /app/kabanbot
EXPOSE 8080
ENTRYPOINT ["/app/kabanbot"]
