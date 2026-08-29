FROM docker.io/golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build

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
