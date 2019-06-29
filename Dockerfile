FROM alpine:3.8

RUN apk add --no-cache ca-certificates

COPY . /app
VOLUME ["/app"]
CMD ["/app/app"]
