FROM golang:1.21-alpine AS builder

# The classic builder keeps this stage as an untagged image; the label lets
# TestBuildAndRunWithBuildContext remove it.
LABEL dockertest.test=build-context-builder

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod ./

# Copy source code
COPY main.go ./

# Build the application
RUN go build -o hello .

# Final stage
FROM alpine:latest

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/hello .

# Run the application
CMD ["./hello"]
