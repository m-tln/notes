FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates openssl

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags='-w -s' -o sidecar .

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata openssl && \
    update-ca-certificates

RUN addgroup -g 1001 -S appuser && \
    adduser -u 1001 -S appuser -G appuser

WORKDIR /app

COPY --from=builder /app/sidecar .

RUN mkdir -p /certs /app/certs && \
    chown -R appuser:appuser /app /certs

USER appuser

EXPOSE 8443

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider https://localhost:8443/health --no-check-certificate || exit 1

CMD ["./sidecar"]