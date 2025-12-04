# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o fair-service ./cmd/fair-service

# Run stage
FROM gcr.io/distroless/static-debian12

COPY --from=builder /app/fair-service /usr/local/bin/fair-service

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/fair-service"]
