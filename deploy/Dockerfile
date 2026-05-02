FROM golang:1.22-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make nodejs npm

# Copy source
COPY . .

# Build dashboard (if source exists)
RUN if [ -d "dashboard" ] && [ -f "dashboard/package.json" ]; then \
      cd dashboard && npm ci && npm run build && \
      mkdir -p static/dashboard && cp -r dist/* static/dashboard/; \
    fi

# Download Go dependencies
RUN go mod download

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /nexus ./main.go

# Final stage — minimal image
FROM gcr.io/distroless/static-debian12

COPY --from=builder /nexus /nexus
COPY --from=builder /build/connectors.d /connectors.d

EXPOSE 8080

ENTRYPOINT ["/nexus"]
