FROM alpine:latest AS builder

# The classic builder keeps this stage as an untagged image; the label lets
# TestBuildAndRunWithBuildContext remove it.
LABEL dockertest.test=build-context-builder

WORKDIR /app

# Copy the script from the build context
COPY hello.sh ./
RUN chmod +x hello.sh

# Final stage
FROM alpine:latest

WORKDIR /app

# Copy the script from the builder stage
COPY --from=builder /app/hello.sh .

# Run the application
CMD ["./hello.sh"]
