FROM docker.io/golang:1.23.7-alpine AS build

ARG GITLAB_SYNC_VERSION="dev"

WORKDIR /app

COPY go.mod .
COPY ./cmd/ ./cmd/
COPY ./mirroring/ ./mirroring/
COPY ./utils/ ./utils/

RUN go mod tidy && \
    go build -ldflags "-X 'main.version=${GITLAB_SYNC_VERSION}'" -o /app/bin/gitlab-sync ./cmd/main.go

FROM docker.io/alpine:latest

RUN adduser -u 1000 -D -S gitlab-sync

COPY --from=build --chown=gitlab-sync --chmod=550 /app/bin/gitlab-sync /usr/local/bin/gitlab-sync

ENTRYPOINT [ "/usr/local/bin/gitlab-sync" ]
