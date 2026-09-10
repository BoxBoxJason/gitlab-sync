FROM docker.io/golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build

WORKDIR /app

COPY . .

ENV GO111MODULE=on \
    CGO_ENABLED=0

RUN apk add --no-cache make git && \
  make build

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS security_provider

# /staging/tmp is staged here so the scratch image below gets a writable /tmp:
# when the destination instance is not premium (or --destination-force-freemium is
# used), repositories are mirrored by cloning them into a temporary directory and
# pushing them to the destination.
RUN apk add --no-cache ca-certificates \
    && addgroup -S gitlab-sync \
    && adduser -S gitlab-sync -G gitlab-sync \
    && mkdir -p /staging/tmp \
    && chmod 1777 /staging/tmp

FROM scratch

COPY --from=security_provider /etc/passwd /etc/passwd
COPY --from=security_provider /etc/group /etc/group
# Required to talk to HTTPS GitLab instances and to clone/push over HTTPS.
COPY --from=security_provider /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=security_provider --chmod=1777 /staging/tmp /tmp

USER gitlab-sync

COPY --from=build /app/bin/gitlab-sync /usr/local/bin/gitlab-sync

ENTRYPOINT [ "/usr/local/bin/gitlab-sync" ]
