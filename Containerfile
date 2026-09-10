FROM docker.io/golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build

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
#
# /staging/cache is the counterpart for --cache-dir (GITLAB_SYNC_CACHE_DIR): it gives
# the image a writable mount point for the volume that keeps the clones between runs.
RUN apk add --no-cache ca-certificates \
    && addgroup -S gitlab-sync \
    && adduser -S gitlab-sync -G gitlab-sync \
    && mkdir -p /staging/tmp /staging/cache \
    && chmod 1777 /staging/tmp /staging/cache

FROM scratch

COPY --from=security_provider /etc/passwd /etc/passwd
COPY --from=security_provider /etc/group /etc/group
# Required to talk to HTTPS GitLab instances and to clone/push over HTTPS.
COPY --from=security_provider /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# The whole staging tree is copied at once, on purpose: COPY applies --chmod to the
# files it copies, never to the destination directory itself, so copying /staging/tmp
# onto /tmp would leave /tmp owned by root with the default 0755 and unwritable by the
# unprivileged user below. Copying the parent instead recreates tmp/ and cache/ as
# entries of the tree, which keeps the 1777 they were staged with.
COPY --from=security_provider /staging/ /

USER gitlab-sync

COPY --from=build /app/bin/gitlab-sync /usr/local/bin/gitlab-sync

ENTRYPOINT [ "/usr/local/bin/gitlab-sync" ]
