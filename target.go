package rabbitmq_consumer_bridge

import (
	"fmt"

	"github.com/go-errors/errors"
	"github.com/streadway/amqp"
)

type Target interface {
	handle(m *amqp.Delivery) ([]byte, error)
	start() error
	terminate() error
}

func NewTarget(service *ServiceConfig) (Target, error) {
	if nil == service.Target {
		return NewHttpTarget(service, app.config.HttpClient.Get())
	}

	switch service.Target.Type {
	case "rabbitmq":
		return NewRabbitMqTarget(service.Target)

	case "http":
		return NewHttpTarget(service, app.config.HttpClient.Get())

	case "lambda":
		return NewLambdaTarget(service.Name, app.config.Lambda)

	case "kafka":
		return NewKafkaTarget(app.config, service)

	case "process":
		return NewProcessTarget(service.Name, service.Target.Process)

	default:
		return nil, errors.New(fmt.Sprintf("unsupported target: %s", service.Target.Type))
	}
}
