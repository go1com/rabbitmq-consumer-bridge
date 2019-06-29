package rabbitmq_consumer_bridge

import (
	"errors"
	"github.com/streadway/amqp"
)

type RabbitMqTargetConfig struct {
	URL      string `yaml:"url"`
	Exchange string `yaml:"exchange"`
	Kind     string `yaml:"kind"`
}

type RabbitMqTarget struct {
	cnf        *RabbitMqTargetConfig
	Connection *amqp.Connection
	Channel    *amqp.Channel
}

func NewRabbitMqTarget(cnf *TargetConfig) (Target, error) {
	if "" == cnf.RabbitMq.Kind {
		cnf.RabbitMq.Kind = "topic"
	}

	return &RabbitMqTarget{cnf: cnf.RabbitMq}, nil
}

func (t *RabbitMqTarget) start() error {
	conn, err := connection(t.cnf.URL)
	if nil != err {
		return err
	}

	t.Connection = conn
	t.Channel = channel(conn, t.cnf.Kind, t.cnf.Exchange)

	return nil
}

func (t *RabbitMqTarget) handle(m *amqp.Delivery) ([]byte, error) {
	msg := amqp.Publishing{
		ContentType: m.ContentType,
		Body:        m.Body,
		Headers:     m.Headers,
	}

	err := t.Channel.Publish(t.cnf.Exchange, m.RoutingKey, false, false, msg)
	if nil == err {
		return nil, nil
	}

	return nil, errors.New("failed pushing")
}

func (t *RabbitMqTarget) terminate() error {
	if nil != t.Channel {
		if err := t.Channel.Close(); err != nil {
			return err
		}
		t.Channel = nil
	}

	if nil != t.Connection {
		if err := t.Connection.Close(); err != nil {
			return err
		}
		t.Connection = nil
	}

	return nil
}
