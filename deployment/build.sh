
#!/bin/bash
SCRIPT_DIR=$(dirname "$0")
echo $SCRIPT_DIR

pushd $SCRIPT_DIR

PLATFORM=linux/amd64

# which docker
# build
docker buildx inspect iot || docker buildx create --name=iot --driver=docker-container --platform linux/amd64,linux/arm64                    
docker buildx build --platform $PLATFORM \
    -t "ghcr.io/andorean/ot-video-analyze/reader-demo:0.0.0" -f docker/Dockerfile \
    .. --push

popd   