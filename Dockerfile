FROM golang:1.26-alpine AS builder

ARG DNS_BUILD_TAGS=""

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    ${DNS_BUILD_TAGS:+-tags "${DNS_BUILD_TAGS}"} \
    -o /sentinel-manager ./cmd/sentinel-manager

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /sentinel-manager /usr/local/bin/sentinel-manager

EXPOSE 8000
ENTRYPOINT ["sentinel-manager"]
CMD ["-config", "/etc/sentinel-manager/config.yaml"]
