End service implementation
====

With #consumer which is built by Golang, each micro service no longer need implements their own RabbitMQ consumer,
especially in PHP, it's really hard to build a long-running process without issue. Instead, they should:

1. Register with #consume
2. Implement `POST /consume` which process the incoming messages.

```php
<?php
/** @var go1\app\App $app */
$app->post('/consume', function(Symfony\Component\HttpFoundation\Request $req) {
    $routingKey = $req->get('routingKey');
    $body = $req->get('body');
    $result = call_user_func('the_consuming_logic', $routingKey, $body);

    return new Symfony\Component\HttpFoundation\JsonResponse(null, $result ? 200 : 500);
});
```
