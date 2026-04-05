#!/bin/bash

# --- ConverseAI Manual Deployment Script ---
# Use this if docker-compose is not installed on your server.

if [ -f .env ]; then
    export $(grep -v '^#' .env | xargs)
fi

HOST_PORT_APP=${HOST_PORT_APP:-8080}
HOST_PORT_DB=${HOST_PORT_DB:-27018}
MONGO_URI=${MONGO_URI:-mongodb://converseai-db:27017}
DB_NAME=${DB_NAME:-ai_chat}
JWT_SECRET=${JWT_SECRET:-your-secret-key}
OLLAMA_BASE_URL=${OLLAMA_BASE_URL:-http://host.docker.internal:11434}
MINIO_ENDPOINT=${MINIO_ENDPOINT:-converseai-storage:9000}
CHROMA_URL=${CHROMA_URL:-http://converseai-vector:8000}

# MinIO Config
MINIO_ROOT_USER=${MINIO_ROOT_USER:-admin}
MINIO_ROOT_PASSWORD=${MINIO_ROOT_PASSWORD:-password123}
MINIO_BUCKET=${MINIO_BUCKET:-converseai}

echo "🚀 Starting ConverseAI manual deployment..."

docker network create converseai-net 2>/dev/null || true

# 1. Start Database
echo "📦 Starting Database (converseai-db)..."
docker stop converseai-db 2>/dev/null || true
docker rm converseai-db 2>/dev/null || true
docker run -d \
    --name converseai-db \
    --network converseai-net \
    -p $HOST_PORT_DB:27017 \
    -v converseai_db_data:/data/db \
    --restart always \
    mongo:latest

# 2. Start Storage (MinIO)
echo "📂 Starting Storage (minio)..."
docker stop converseai-storage 2>/dev/null || true
docker rm converseai-storage 2>/dev/null || true
docker run -d \
    --name converseai-storage \
    --network converseai-net \
    -p 9000:9000 \
    -p 9001:9001 \
    -e MINIO_ROOT_USER=$MINIO_ROOT_USER \
    -e MINIO_ROOT_PASSWORD=$MINIO_ROOT_PASSWORD \
    -v converseai_storage_data:/data \
    --restart always \
    minio/minio server /data --console-address ":9001"

# 3. Start Vector DB (ChromaDB)
echo "🧬 Starting Vector DB (chromadb)..."
docker stop converseai-vector 2>/dev/null || true
docker rm converseai-vector 2>/dev/null || true
docker run -d \
    --name converseai-vector \
    --network converseai-net \
    -p 8000:8000 \
    -v converseai_vector_data:/index_data \
    --restart always \
    chromadb/chroma:latest

# 4. Build Application
echo "🛠️ Building Application Image (converseai)..."
docker build -t converseai .

# 5. Stop existing container if running
docker stop converseai 2>/dev/null || true
docker rm converseai 2>/dev/null || true

# 6. Start Application
echo "🌐 Starting Application on port $HOST_PORT_APP..."
docker run -d \
    --name converseai \
    --network converseai-net \
    -p $HOST_PORT_APP:8080 \
    -e PORT=8080 \
    -e MONGO_URI=$MONGO_URI \
    -e DB_NAME=$DB_NAME \
    -e JWT_SECRET=$JWT_SECRET \
    -e OLLAMA_BASE_URL=$OLLAMA_BASE_URL \
    -e MINIO_ENDPOINT=$MINIO_ENDPOINT \
    -e MINIO_ROOT_USER=$MINIO_ROOT_USER \
    -e MINIO_ROOT_PASSWORD=$MINIO_ROOT_PASSWORD \
    -e MINIO_BUCKET=$MINIO_BUCKET \
    -e MINIO_USE_SSL=false \
    -e CHROMA_URL=$CHROMA_URL \
    -e EMBEDDING_MODEL=$EMBEDDING_MODEL \
    -e DEFAULT_PLANNER_MODEL=$DEFAULT_PLANNER_MODEL \
    -e DEFAULT_CHAT_MODEL=$DEFAULT_CHAT_MODEL \
    -e DEFAULT_OCR_MODEL=$DEFAULT_OCR_MODEL \
    -e DEFAULT_VISION_MODEL=$DEFAULT_VISION_MODEL \
    -e DEFAULT_CODING_MODEL=$DEFAULT_CODING_MODEL \
    -e DEFAULT_TRANSLATION_MODEL=$DEFAULT_TRANSLATION_MODEL \
    --add-host=host.docker.internal:host-gateway \
    --restart always \
    converseai

echo "✅ Done! Application is running at http://localhost:$HOST_PORT_APP"
echo "📂 MinIO Console is at http://localhost:9001 (User: $MINIO_ROOT_USER, Pass: $MINIO_ROOT_PASSWORD)"
echo "🧬 ChromaDB is running at http://localhost:8000"
