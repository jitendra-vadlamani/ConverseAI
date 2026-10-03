# --- Stage 1: build the React client ---
FROM node:22.20.0-alpine3.22 AS frontend
WORKDIR /app/client
COPY client/package.json client/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY client/ ./
RUN npm run build

# --- Stage 2: build the Go server (embeds the client) ---
FROM golang:1.25.14-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY internal ./internal
COPY --from=frontend /app/client/dist ./client/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/converseai .

# --- Stage 3: minimal runtime, non-root ---
FROM alpine:3.22.6
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 converseai
COPY --from=backend /out/converseai /usr/local/bin/converseai
USER 10001
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["converseai"]
CMD ["serve"]
