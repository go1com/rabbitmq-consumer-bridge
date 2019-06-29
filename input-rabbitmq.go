package rabbitmq_consumer_bridge

import (
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

type RabbitMqConnectionOption struct {
	Url string `yaml:"url"`

	con *amqp.Connection
}

func (c RabbitMqConnectionOption) Connection() (*amqp.Connection) {
	var err error

	if nil == c.con {
		c.con, err = connection(c.Url)

		if nil != err {
			logrus.
				WithError(err).
				WithField("component", "input-rabbitmq").
				Panic("can not create connection")
		}
	}

	return c.con
}
