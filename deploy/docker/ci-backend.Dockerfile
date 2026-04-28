FROM 192.0.2.10:5000/golang:1.26-alpine

ENV GOPROXY=https://goproxy.cn,direct
ENV GONOSUMCHECK=*
ENV PATH=/go/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

RUN apk add --no-cache \
    ca-certificates \
    docker-cli \
    docker-cli-buildx \
    git \
    tzdata
