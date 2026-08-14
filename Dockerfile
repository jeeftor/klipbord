# Build stage
FROM golang:1.26-alpine3.22 AS builder

WORKDIR /build
COPY go.mod ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -o klipbord -ldflags="-s -w -X main.version=${VERSION}" .

# Final stage
FROM alpine:3.22

RUN apk add --no-cache ca-certificates ffmpeg su-exec

COPY --from=builder /build/klipbord /klipbord
COPY entrypoint.sh /entrypoint.sh

ENV PORT=8080
ENV DATA_DIR=/data
ENV BASE_URL=http://localhost:8080
ENV MAX_UPLOAD_MB=2048

RUN addgroup -S -g 10001 klipbord \
    && adduser -S -D -H -u 10001 -G klipbord klipbord \
    && mkdir -p /data \
    && chown klipbord:klipbord /data \
    && chmod +x /entrypoint.sh

# Declare /data as a volume so Docker preserves it across container updates.
# Mount a named volume or bind mount here to persist settings and uploads.
VOLUME /data

EXPOSE 8080

# The entrypoint runs as root to fix /data ownership on bind mounts,
# then drops to the non-root klipbord user (uid 10001) before starting.
ENTRYPOINT ["/entrypoint.sh"]
CMD ["/klipbord"]
