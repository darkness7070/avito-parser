FROM golang:1.21-alpine AS builder

# Install dependencies for Rod/Chromium
RUN apk add --no-cache chromium

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

# Install chromium and ca-certificates
RUN apk --no-cache add ca-certificates chromium

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/main .

# Create logs directory
RUN mkdir -p /app/logs

# Set environment variable for Rod to use system Chromium
ENV ROD_LAUNCHER_BIN=/usr/bin/chromium-browser

CMD ["./main"]