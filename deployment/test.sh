#!/bin/bash
SCRIPT_DIR=$(dirname "$0")
echo $SCRIPT_DIR

pushd $SCRIPT_DIR

DEMO_BASE_URL=
DEMO_CLIENT_ID=
DEMO_USER=
DEMO_PASSWORD=


docker run -it --rm --name ot-reader-test \
    -e DEMO_BASE_URL=$DEMO_BASE_URL \
    -e DEMO_CLIENT_ID=$DEMO_CLIENT_ID \
    -e DEMO_USER=$DEMO_USER \
    -e DEMO_PASSWORD=$DEMO_PASSWORD \
    -p "9119:9999" \
    ghcr.io/andorean/ot-video-analyze/reader-demo:0.0.0 