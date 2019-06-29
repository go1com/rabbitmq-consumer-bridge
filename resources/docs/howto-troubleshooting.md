Trouble shooting
====

## Failed to deploy

- Use graylog, use your image name as keyword.

## Message debugging

If the message is broken, from the given log, we can manual curl command to request to the service, but it's faster with
the simple script: https://code.go1.com.au/snippets/568

## Exporting metrics

- Use branch 3.x
- Tell prometheus to scrape your metrics. Ref https://code.go1.com.au/server/prometheus/


## Checking errors

- Graylog query examples:
  - `"level=error" AND "service failed handling"`
    - on left hand side, select field `image_name` > click drop down icon > Select "Quick values"
      - We can see list of domain consumers which is generating errors.
  - `"level=error" AND "recovered from panic"`

## My queue is being stucked, how to debug

### Using consumer IP address

1. Find out the name of stucking queue
2. Under consumer, get the consumer's IP address as keyword

### Query for error using domain consumer

> container_name:ecs-ecscompose-lo-index-consumer-dev-* AND "level=error"
