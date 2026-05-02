#!/bin/bash

# Build script for Synapse Docker images
# Usage: ./docker/build.sh

set -e

echo "Building synapse/agent-claude image..."
docker build -t synapse/agent-claude:latest -t synapse/agent-claude:0.1.0 docker/agent-claude/

echo "Building synapse/agent-cursor image..."
docker build -t synapse/agent-cursor:latest -t synapse/agent-cursor:0.1.0 docker/agent-cursor/

echo ""
echo "Images built successfully:"
echo "  - synapse/agent-claude:latest"
echo "  - synapse/agent-claude:0.1.0"
echo "  - synapse/agent-cursor:latest"
echo "  - synapse/agent-cursor:0.1.0"

echo ""
echo "To push to registry:"
echo "  docker push synapse/agent-claude:latest"
echo "  docker push synapse/agent-claude:0.1.0"
echo "  docker push synapse/agent-cursor:latest"
echo "  docker push synapse/agent-cursor:0.1.0"