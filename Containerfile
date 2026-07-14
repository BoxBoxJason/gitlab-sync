FROM docker.io/golang:1.26.4-alpine@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS build

WORKDIR /app

COPY . .

ENV GO111MODULE=on \
    CGO_ENABLED=0

RUN apk add --no-cache make git && \
  make build

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS security_provider

RUN addgroup -S gitlab-sync \
    && adduser -S gitlab-sync -G gitlab-sync

FROM scratch

COPY --from=security_provider /etc/passwd /etc/passwd

USER gitlab-sync

COPY --from=build /app/bin/gitlab-sync /usr/local/bin/gitlab-sync

ENTRYPOINT [ "/usr/local/bin/gitlab-sync" ]
