FROM alpine:latest AS builder

# The classic builder keeps this stage as an untagged image; the label lets
# TestBuildAndRunWithBuildContext remove it. DOCKERTEST_RUN keeps test
# processes sharing a daemon from removing each other's image.
ARG DOCKERTEST_RUN
LABEL dockertest.test=build-context-builder-${DOCKERTEST_RUN}

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
