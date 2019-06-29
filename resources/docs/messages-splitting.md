Messages splitting
=======

We have very limited consumers to process huge messages, they are very easy to be flooded. To resolve the problem we
generate utility queues/consumers and route messages.

### Utility queues/consumers

We can enable splitting message via configuration for each service.

```yaml
services:
      - name:  "event"
        queue: "event-service"
        split: 100
```

Then 100 queues will be generated with pattern:

    [service.queue]:{0-99}

When a message is published to the queue, consumer will parse the message payload 
to looking for `id` field (expected its type is numeric. Default value is 0). Finally consumer will calculate
modulus between `id` value and number of split 


### Benchmark

Take 115 seconds to process

    php tests/benchmark/produce-messages-without-group.php
    
Take 3 seconds to process

    php tests/benchmark/produce-messages-with-group.php
