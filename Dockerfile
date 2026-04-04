FROM golang:1.26.1-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/dockerdns .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates
COPY --from=build /out/dockerdns /usr/local/bin/dockerdns

EXPOSE 53/udp 53/tcp

ENTRYPOINT ["/usr/local/bin/dockerdns"]
