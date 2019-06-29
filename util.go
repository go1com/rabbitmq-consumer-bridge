package rabbitmq_consumer_bridge

import (
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

func Env(name string, defaultValue string) string {
	if value := os.Getenv(name); "" != value {
		return value
	}

	return defaultValue
}

func connection(queueUrl string) (*amqp.Connection, error) {
	con, err := amqp.Dial(queueUrl)

	if nil != err {
		logrus.
			WithError(err).
			Error("failed to make connection")

		return nil, err
	}

	go func() {
		conCloseChan := con.NotifyClose(make(chan *amqp.Error))

		select
		{
		case err := <-conCloseChan:
			// Don't restart the consumer, just terminate the application.
			// The monitor should consume application exit.
			logrus.
				WithError(err).
				Errorln("connection broken")

			app.stop <- true
		}
	}()

	return con, nil
}

func channel(con *amqp.Connection, kind string, exchangeName string) *amqp.Channel {
	ch, err := con.Channel()
	if nil != err {
		logrus.WithError(err).Panic("failed to make channel")
	}

	if "topic" != kind && "direct" != kind {
		panic("unsupported channel kind: " + kind)
	}

	err = ch.ExchangeDeclare(exchangeName, kind, false, false, false, false, nil)
	if nil != err {
		panic(err.Error())
	}

	return ch
}

func stream(ch *amqp.Channel, exchange string, queue string, routingKeys []string) <-chan amqp.Delivery {
	defer func() {
		logrus.
			WithField("exchange", exchange).
			WithField("consumer", queue).
			WithField("routingKeys", routingKeys).
			Info("consumer started")

		app.chConsumerStart <- true
	}()

	_, err := ch.QueueDeclare(queue, false, false, false, false, nil)
	if nil != err {
		logrus.
			WithError(err).
			WithField("queue", queue).
			WithField("routingKeys", routingKeys).
			Panic("failed declaring queue")
	}

	for _, routingKey := range routingKeys {
		ch.QueueBind(queue, routingKey, exchange, true, nil)
	}

	err = ch.Qos(1, 0, false)
	if nil != err {
		logrus.
			WithError(err).
			WithField("queue", queue).
			WithField("routingKeys", routingKeys).
			Panic("failed to setup qos")
	}

	messages, err := ch.Consume(queue, "", false, false, false, true, nil)
	if nil != err {
		panic(err.Error())
	}

	return messages
}

func push(queue string, service string, m *amqp.Delivery) bool {
	start := time.Now()
	result := func() bool {
		switch service {
		case "consumer":
			return app.pushDynamic(service, m)

		case "lazy":
			keys := strings.Split(m.RoutingKey, ".")
			serviceName := keys[1] // do.SERVICE_NAME.# -> SERVICE_NAME

			if serviceName == "consumer" {
				return app.pushDynamic(service, m)
			}

			return app.push(serviceName, m)

		default:
			return app.push(service, m)
		}
	}()

	if result {
		promDurationHistogram.
			WithLabelValues(queue, service, m.RoutingKey).
			Observe(time.Since(start).Seconds())

		promSuccessMessageCounter.
			WithLabelValues(queue, service, m.RoutingKey).
			Inc()

		logrus.
			WithField("queue", queue).
			WithField("service", service).
			WithField("message.routingKey", m.RoutingKey).
			WithField("message.body", string(m.Body)).
			Info("executing microservice")

		return true
	}

	return false
}

func convert(m *amqp.Delivery) amqp.Publishing {
	return amqp.Publishing{
		ContentType: m.ContentType,
		Body:        m.Body,
		Headers:     m.Headers,
	}
}

func _az() []string {
	return []string{"_", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z"}
}

func _entityTypes() []string {
	return []string{"user", "lo", "enrolment", "other"}
}

func firstChar(name string) string {
	_az := _az()

	if vInt, err := strconv.Atoi(name); err == nil {
		vInt = vInt % len(_az)

		return _az[vInt]
	}

	name = strings.ToLower(name[0:1])
	if !hasElement(_az, name) {
		return "_"
	}

	return name
}

func hasElement(s interface{}, elem interface{}) bool {
	arrV := reflect.ValueOf(s)

	if arrV.Kind() == reflect.Slice {
		for i := 0; i < arrV.Len(); i++ {
			if arrV.Index(i).Interface() == elem {
				return true
			}
		}
	}

	return false
}

func getPath(name string, env string, pattern string) string {
	pattern = strings.Replace(pattern, "SERVICE", name, -1)
	pattern = strings.Replace(pattern, "ENVIRONMENT", env, -1)

	return pattern
}

func loop(
	terminate chan bool,
	stream <-chan amqp.Delivery,
	service *ServiceConfig,
	handler func(m *amqp.Delivery),
	nack func(uuid string, m amqp.Delivery, failedValidation bool),
) {
NEXT:
	for {
		select {
		case <-terminate:
			return

		case m, received := <-stream:
			if !received {
				break
			}

			if nil == m.Headers {
				m.Headers = amqp.Table{}
			}

			uuid := ""
			if nil != m.Headers["X-UUID"] {
				uuid = m.Headers["X-UUID"].(string)
			}

			// Ignore if message doesn't pass the condition
			if nil != service {
				for _, route := range service.Routes {
					if route.Name == m.RoutingKey && nil != route.Condition && !route.Condition.validate(&m) {
						nack(uuid, m, true)
						continue NEXT
					}
				}
			}

			handler(&m)
		}
	}
}

func retry(
	process func() bool,
	onError func(), onSuccess func(),
) {
	if ok := process(); ok {
		onSuccess()
	} else {
		onError()
	}
}

func part(m *amqp.Delivery, path string) []byte {
	if "body" == path {
		return m.Body
	}

	if strings.HasPrefix(path, "headers.") {
		header := strings.Split(path, ".")[1]
		if nil != m.Headers[header] {
			return []byte(m.Headers[header].(string))
		}
	}

	return []byte("")
}
