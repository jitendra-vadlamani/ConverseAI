#!/bin/bash

# --- ConverseAI Manual Deployment Script ---
# Use this if docker-compose is not installed on your server.

# Load environment variables from .env
if [ -f .env ]; then
    export $(grep -v '^#' .env | xargs)
fi

# Set defaults if not provided in .env
HOST_PORT_APP=${HOST_PORT_APP:-8080}
MONGO_URI=${MONGO_URI:-mongodb://converseai-db:27017}
DB_NAME=${DB_NAME:-ai_chat}
JWT_SECRET=${JWT_SECRET:-your-secret-key}
OLLAMA_BASE_URL=${OLLAMA_BASE_URL:-http://host.docker.internal:11434}

echo "🚀 Starting ConverseAI manual deployment..."

# 1. Create dedicated network
docker network create converseai-net 2>/dev/null || true

# 2. Start Database
echo "📦 Starting Database (converseai-db)..."
docker run -d \
    --name converseai-db \
    --network converseai-net \
    -v converseai_db_data:/data/db \
    --restart always \
    mongo:latest

# 3. Build Application
echo "🛠️ Building Application Image (converseai)..."
docker build -t converseai .

# 4. Stop existing container if running
docker stop converseai 2>/dev/null || true
docker rm converseai 2>/dev/null || true

# 5. Start Application
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
    --add-host=host.docker.internal:host-gateway \
    --restart always \
    converseai

echo "✅ Done! Application is running at http://localhost:$HOST_PORT_APP"
