#!/bin/bash

REPO_URL="https://gitlab.com/ming2k/llm-gateway.git"

# Check if the repository directory exists
if [ -d "$REPO_DIR" ]; then
    echo "Repository directory exists. Pulling latest changes..."
    cd "$REPO_DIR"
    git pull
    cd ..
else
    echo "Repository directory does not exist. Cloning..."
    git clone "$REPO_URL" .
fi

# Build new image
echo "Building Docker image..."
docker build -t llm-gateway:latest .

# Stop old container
echo "Stopping old containers..."
docker-compose down

# Start new container
echo "Starting new containers..."
docker-compose up -d

echo "Deployment completed successfully."