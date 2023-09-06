FROM --platform=$BUILDPLATFORM golang:alpine3.18 as builder


WORKDIR /code
COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \   
    go build -o demo cmd/test/xprotect.go

FROM alpine:3.18 as image
COPY --from=builder /code/demo /usr/local/bin/demo
ADD public /public
COPY deployment/entrypoint.sh /entrypoint.sh
EXPOSE 9999 

# ENTRYPOINT ["demo"]
ENTRYPOINT ["/entrypoint.sh"]