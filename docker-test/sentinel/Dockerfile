ARG VALKEY_VERSION=7.2
FROM golang:1.26-alpine AS agent-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /sentinel-agent ./cmd/sentinel-agent

FROM valkey/valkey:${VALKEY_VERSION}
COPY --from=agent-builder /sentinel-agent /usr/local/bin/sentinel-agent
RUN ln -sf /usr/local/bin/sentinel-agent /usr/local/bin/sentinel-agent-reconfig && \
    ln -sf /usr/local/bin/sentinel-agent /usr/local/bin/sentinel-agent-notify

COPY docker-test/sentinel/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 26379
ENTRYPOINT ["/entrypoint.sh"]
