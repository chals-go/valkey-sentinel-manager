FROM golang:1.26-alpine AS builder
WORKDIR /src
RUN go mod init mock-dns
COPY main.go .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /mock-dns .

FROM alpine:3.21
COPY --from=builder /mock-dns /usr/local/bin/mock-dns
EXPOSE 8080
CMD ["mock-dns"]
