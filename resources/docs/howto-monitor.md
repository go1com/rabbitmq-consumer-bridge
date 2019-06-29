How to monitor
====

The service provide metrics in prometheus format, so that we can monitor it using
grafana.

## Example

Monitor retry of `video-li` service:

    consumer_total_retry_message{service="video-li"}
