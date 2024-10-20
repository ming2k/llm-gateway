#!/bin/bash

REPO_URL="https://gitlab.com/ming2k/llm-gateway.git"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"


# Check if the repository directory exists
if [ -d "$REPO_DIR" ]; then
    echo "Repository directory exists. Pulling latest changes..."
    cd "$REPO_DIR"
    git pull
    cd ..
else
    echo "Repository directory does not exist. Cloning..."
    git clone "$REPO_URL"
fi

# Build new image
echo "Building Docker image..."
docker build -t $APP_NAME:latest ./$APP_NAME

# Stop old container
echo "Stopping old containers..."
docker compose down -f ./$APP_NAME/docker-compose.yml

# Start new container
echo "Starting new containers..."
docker compose up -d -f ./$APP_NAME/docker-compose.yml

echo "Deployment completed successfully."