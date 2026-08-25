# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src

ARG VERSION=0.0.0-dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w \
      -X 'github.com/silverbp/ava/internal/version.Version=${VERSION}' \
      -X 'github.com/silverbp/ava/internal/version.GitCommit=${GIT_COMMIT}' \
      -X 'github.com/silverbp/ava/internal/version.BuildDate=${BUILD_DATE}'" \
    -o /out/ava ./cmd/ava

FROM alpine:latest
RUN apk add --no-cache ca-certificates && \
    adduser -D -H -u 10001 ava
COPY --from=build /out/ava /usr/local/bin/ava

USER ava
EXPOSE 9090 9091
ENTRYPOINT ["/usr/local/bin/ava"]
