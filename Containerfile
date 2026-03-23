FROM docker.io/golang:1.25.8-alpine AS build

WORKDIR /app

COPY . .

ENV GO111MODULE=on \
    CGO_ENABLED=0

RUN apk add --no-cache make git && \
  make build

FROM alpine:3.23.3 AS security_provider

RUN addgroup -S gitlab-sync \
    && adduser -S gitlab-sync -G gitlab-sync

FROM scratch

COPY --from=security_provider /etc/passwd /etc/passwd

USER gitlab-sync

COPY --from=build /app/bin/gitlab-sync /usr/local/bin/gitlab-sync

ENTRYPOINT [ "/usr/local/bin/gitlab-sync" ]
