FROM docker.io/golang:1.24.2-alpine AS build

ARG VERSION="dev"

WORKDIR /app

COPY go.mod .
COPY ./cmd/ ./cmd/
COPY ./internal/ ./internal/

ENV GO111MODULE=on \
    CGO_ENABLED=0

RUN go mod tidy && \
    go build -ldflags "-X 'main.version=${VERSION}'" -o /app/bin/gitlab-sync ./cmd/main.go

FROM scratch

ARG BUILD_DATE="1970-01-01T00:00:00Z" \
    VERSION="dev" \
    REVISION="dev"

LABEL org.opencontainers.image.title="gitlab-sync" \
    org.opencontainers.image.description="synchronize GitLab projects and groups between two GitLab instances" \
    org.opencontainers.image.source="${SOURCE}" \
    org.opencontainers.image.url="ghcr.io/boxboxjason/gitlab-sync" \
    org.opencontainers.image.created="${BUILD_DATE}" \
    org.opencontainers.image.revision="${REVISION}" \
    org.opencontainers.image.version="${VERSION}" \
    org.opencontainers.image.vendor="boxboxjason"

COPY --from=build /app/bin/gitlab-sync /usr/local/bin/gitlab-sync

ENTRYPOINT [ "/usr/local/bin/gitlab-sync" ]
