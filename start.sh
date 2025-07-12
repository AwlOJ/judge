#!/bin/bash

dockerd &

echo "Starting Docker daemon..."
while ! docker info > /dev/null 2>&1; do
    echo "Waiting for Docker daemon to start..."
    sleep 2
done

echo "Docker daemon started successfully"

echo "Building judge images..."
./build-images.sh

echo "Starting judge application..."
exec ./judge