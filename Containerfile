FROM docker.io/golang:1.25.3-alpine AS build

ARG VERSION="dev"

WORKDIR /app

COPY go.mod .
COPY ./cmd/ ./cmd/
COPY ./pkg/ ./pkg/
COPY ./internal/ ./internal/

ENV GO111MODULE=on \
    CGO_ENABLED=0

RUN go mod tidy && \
    go build -ldflags "-X 'main.version=${VERSION}'" -o /app/bin/gitlab-sync ./cmd/main.go

FROM alpine:3.22.2 AS security_provider

RUN addgroup -S gitlab-sync \
    && adduser -S gitlab-sync -G gitlab-sync

FROM scratch

COPY --from=security_provider /etc/passwd /etc/passwd

USER gitlab-sync

COPY --from=build /app/bin/gitlab-sync /usr/local/bin/gitlab-sync

ENTRYPOINT [ "/usr/local/bin/gitlab-sync" ]
