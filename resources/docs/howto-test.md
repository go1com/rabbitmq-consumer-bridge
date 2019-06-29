Testing on local
====

New test cases in Golang are welcome. Reason we wrote test cases in PHP just because that it's not our native speaking language.

    docker run --rm -p 5672:5672 -p 15672:15672 -e RABBITMQ_DEFAULT_USER='go1' -e RABBITMQ_DEFAULT_PASS='go1' rabbitmq:3-management
    export QUEUE_URL=amqp://go1:go1@localhost:5672
    go test 
