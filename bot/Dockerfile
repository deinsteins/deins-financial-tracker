# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go.mod and download dependencies first (caching layer)
COPY go.mod ./
RUN go mod download

# Copy application source code
COPY . .

# Build the Go application
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o bot-service main.go

# Run stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates wget

WORKDIR /app

# Copy the compiled binary from the builder stage
COPY --from=builder /app/bot-service .

# Expose bot service default port
EXPOSE 8080

# Run the binary
CMD ["./bot-service"]
