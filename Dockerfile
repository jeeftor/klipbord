# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /build
COPY go.mod ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -o klipbord -ldflags="-s -w -X main.version=${VERSION}" .

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates

COPY --from=builder /build/klipbord /klipbord

ENV PORT=8080
ENV DATA_DIR=/data
ENV BASE_URL=http://localhost:8080
ENV MAX_UPLOAD_MB=2048

RUN mkdir -p /data

# Declare /data as a volume so Docker preserves it across container updates.
# Mount a named volume or bind mount here to persist settings and uploads.
VOLUME /data

EXPOSE 8080

CMD ["/klipbord"]
