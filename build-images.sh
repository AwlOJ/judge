#!/bin/bash

# Build Docker images for online judge

echo "Building gcc-with-time image..."
docker build -f Dockerfile.gcc-with-time -t gcc-with-time .

echo "Building python-with-time image..."
docker build -f Dockerfile.python-with-time -t python-with-time .

echo "Listing built images..."
docker images | grep -E "(gcc-with-time|python-with-time)"

echo "All images built successfully!"