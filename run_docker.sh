#!/bin/bash

# install library
# apk add build-base linux-headers
docker run -it --rm --name ot-reader-dev \
    -v $(pwd):/go/app \
    -w /go/app \
    -p 9120:9999 \
    golang:alpine3.18 sh 