# --- Stage 1: Build React Frontend ---
FROM node:20-alpine AS frontend-builder
WORKDIR /app/client
COPY client/package*.json ./
RUN npm install
COPY client/ ./
RUN npm run build

# --- Stage 2: Build Go Backend ---
FROM golang:1.25.0-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Copy built frontend assets from Stage 1
COPY --from=frontend-builder /app/client/dist /app/client/dist
RUN go build -o main .

# --- Stage 3: Final Image ---
FROM alpine:latest
WORKDIR /root/
COPY --from=backend-builder /app/main .
EXPOSE 8080
CMD ["./main"]
