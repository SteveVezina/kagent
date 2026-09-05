ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM ${BASE_IMAGE_REGISTRY}/chainguard/go:latest AS builder
ARG TARGETARCH
ARG TARGETOS

WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod,rw \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    go mod download
COPY api/ api/
COPY adk/ adk/
COPY core/ core/
COPY harness/ harness/
COPY pkg/ pkg/
RUN --mount=type=cache,target=/root/go/pkg/mod,rw \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -o /kagent-pi ./harness/pi/cmd

FROM node:24-alpine3.22
ARG PI_CODING_AGENT_VERSION=0.85.0
ARG VERSION

RUN apk add --no-cache bash ca-certificates git \
    && npm install -g --ignore-scripts --omit=dev --no-audit --no-fund \
        "@earendil-works/pi-coding-agent@${PI_CODING_AGENT_VERSION}" \
    && test "$(pi --version)" = "${PI_CODING_AGENT_VERSION}" \
    && pi --help >/dev/null \
    && addgroup -S -g 65532 kagent \
    && adduser -S -D -H -u 65532 -G kagent -h /data -s /sbin/nologin kagent \
    && mkdir -p /data/adapter /data/pi /data/workspace \
    && chown -R 65532:65532 /data

COPY --from=builder /kagent-pi /usr/local/bin/kagent-pi

USER 65532:65532
WORKDIR /data/workspace
EXPOSE 80 8081

LABEL org.opencontainers.image.source="https://github.com/kagent-dev/kagent"
LABEL org.opencontainers.image.description="Kagent Pi native Harness adapter"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/usr/local/bin/kagent-pi"]
