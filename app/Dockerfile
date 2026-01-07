FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates postgresql-client

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o notes-app main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata postgresql-client && \
    update-ca-certificates

RUN addgroup -g 1001 -S appuser && \
    adduser -u 1001 -S appuser -G appuser

WORKDIR /app

COPY --from=builder /app/notes-app .

COPY --from=builder /app/wait-for-it.sh /app/wait-for-it.sh
RUN chmod +x /app/wait-for-it.sh

COPY --from=builder /app/init.sql /app/init.sql

RUN mkdir -p /app/certs && chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=30s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["sh", "-c", "/app/wait-for-it.sh postgres:5432 -- /app/notes-app"]