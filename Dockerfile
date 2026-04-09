FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o tunnel-api ./cmd/server

FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata curl

WORKDIR /app

RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

COPY --from=builder --chown=appuser:appgroup /app/tunnel-api ./tunnel-api

RUN chmod +x ./tunnel-api

USER appuser

EXPOSE 8080
EXPOSE 7001
EXPOSE 25565
EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

CMD ["./tunnel-api"]
