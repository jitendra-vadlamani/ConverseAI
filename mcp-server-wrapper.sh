#!/bin/bash
echo "=== MCP Server Wrapper Started at $(date) ===" >> mcp_debug.log
echo "Cwd: $(pwd)" >> mcp_debug.log
echo "Env MONGO_URI: $MONGO_URI" >> mcp_debug.log
echo "Env CHROMA_URL: $CHROMA_URL" >> mcp_debug.log
echo "Env MINIO_ENDPOINT: $MINIO_ENDPOINT" >> mcp_debug.log
echo "Env MINIO_ROOT_USER: $MINIO_ROOT_USER" >> mcp_debug.log
./mcp-server "$@" 2>> mcp_debug.log
echo "=== MCP Server Exited with code $? at $(date) ===" >> mcp_debug.log
