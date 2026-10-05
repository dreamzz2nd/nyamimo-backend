# Multi-stage build for ultra-lightweight Docker image (~15MB)
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download || true

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o nyamimo-server main.go

# Minimal runtime image
FROM alpine:3.19

WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/nyamimo-server .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/public ./public
COPY --from=builder /app/config.json ./config.json
COPY --from=builder /app/data ./data

ENV PORT=8080
EXPOSE 8080

CMD ["./nyamimo-server"]
