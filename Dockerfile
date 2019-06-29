FROM golang:1.12-alpine

WORKDIR /go/src/github.com/go1com/rabbitmq-consumer-bridge/
COPY    . /go/src/github.com/go1com/rabbitmq-consumer-bridge/

RUN apk add --no-cache git
RUN GO111MODULE=on go mod vendor
RUN CGO_ENABLED=0 GOOS=linux go build -o /app /go/src/github.com/go1com/rabbitmq-consumer-bridge/cmd/main.go

FROM alpine:3.8
RUN apk add --no-cache ca-certificates
COPY --from=0 /app /app
ENTRYPOINT ["/app"]
