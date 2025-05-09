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

COPY --from=build /app/bin/gitlab-sync /usr/local/bin/gitlab-sync

ENTRYPOINT [ "/usr/local/bin/gitlab-sync" ]
