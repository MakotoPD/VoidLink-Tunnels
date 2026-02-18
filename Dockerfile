# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tunnel-api ./cmd/server

# Runtime image
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata curl

WORKDIR /app

RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

COPY --from=builder /app/tunnel-api .
COPY --from=builder /app/wordlist ./wordlist

RUN chown -R appuser:appgroup /app

USER appuser

# REST API
EXPOSE 8080
# Tunnel control channel (VoidLink desktop client connects here)
EXPOSE 7001
# Shared Minecraft TCP proxy
EXPOSE 25565
# Shared HTTP proxy (Dynmap / BlueMap)
EXPOSE 8081

HEALTHCHECK --interval=300s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

CMD ["./tunnel-api"]
