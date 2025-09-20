FROM golang:1.21-alpine AS builder

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

# Install required packages for Chromium and Rod
RUN apk --no-cache add \
    chromium \
    ca-certificates \
    font-noto-emoji \
    freetype \
    harfbuzz \
    ttf-freefont \
    wqy-zenhei

# Create app directory
WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/main .

# Create logs directory
RUN mkdir -p /app/logs

# Create user to run chromium (chromium won't run as root)
RUN addgroup -g 1001 -S appuser && \
    adduser -u 1001 -S appuser -G appuser

# Change ownership of app directory
RUN chown -R appuser:appuser /app

# Set environment variables for Rod
ENV ROD_LAUNCHER_BIN=/usr/bin/chromium-browser
ENV ROD_NO_SANDBOX=true

# Switch to non-root user
USER appuser

CMD ["./main"]