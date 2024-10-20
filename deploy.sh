#!/bin/bash

REPO_URL="https://gitlab.com/ming2k/llm-gateway.git"

# if APP_NAME exists, remove the directory
if [ -d "$APP_NAME" ]; then
    rm -rf $APP_NAME
    echo "Removed existing $APP_NAME directory."
fi

echo "Cloning repository..."
git clone "$REPO_URL"

# Build new image
echo "Building Docker image..."
docker build -t $APP_NAME:latest ./$APP_NAME

# Stop old container
echo "Stopping old containers..."
docker compose -f ./$APP_NAME/docker-compose.yml down

# Start new container
echo "Starting new containers..."
docker compose -f ./$APP_NAME/docker-compose.yml up -d

echo "Deployment completed successfully."