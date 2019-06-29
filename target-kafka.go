package rabbitmq_consumer_bridge

import (
	"errors"
	"time"

	"github.com/Shopify/sarama"
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

type KafkaConnectionOptions struct {
	Servers     []string                `yaml:"servers"`
	ClientID    string                  `yaml:"client-id"`
	Timeout     time.Duration           `yaml:"timeout"`
	AckReplicas sarama.RequiredAcks     `yaml:"ack"`
	Compress    sarama.CompressionCodec `yaml:"compress"`
	Retry       int                     `yaml:"retry"`
}

type KafkaServiceConfig struct {
	Connection string `yaml:"connection"`
	Topic      string `yaml:"topic"`
}

type KafkaTarget struct {
	Client sarama.SyncProducer
	Topic  string
}

func NewKafkaTarget(cnf *AppConfig, service *ServiceConfig) (Target, error) {
	connections := *cnf.Kafka
	connection := service.Target.Kafka.Connection
	if "" == service.Target.Kafka.Connection {
		connection = "default"
	}

	o := connections[connection]
	c := sarama.NewConfig()
	c.ClientID = o.ClientID
	c.Producer.RequiredAcks = o.AckReplicas // Wait for all in-sync replicas to ack the message
	c.Producer.Timeout = o.Timeout
	c.Producer.Compression = o.Compress
	c.Producer.Retry.Max = o.Retry
	producer, _ := sarama.NewSyncProducer(o.Servers, c)

	return &KafkaTarget{Client: producer, Topic: service.Target.Kafka.Topic}, nil
}

func (t *KafkaTarget) start() error { return nil }

func (t *KafkaTarget) terminate() error {
	return t.Client.Close()
}

func (t *KafkaTarget) handle(m *amqp.Delivery) ([]byte, error) {
	msg := &sarama.ProducerMessage{
		Topic:     t.Topic,
		Timestamp: time.Now(),
		Value:     sarama.StringEncoder(m.Body),
		Headers: func() []sarama.RecordHeader {
			headers := []sarama.RecordHeader{}

			if len(m.Headers) > 0 {
				for key, val := range m.Headers {
					headers = append(headers, sarama.RecordHeader{
						Key:   []byte(key),
						Value: val.([]byte),
					})
				}
			}

			return headers
		}(),
	}

	partition, offset, err := t.Client.SendMessage(msg)

	if nil != err {
		logrus.
			WithError(err).
			WithField("component", "target-kafka").
			WithField("partition", partition).
			WithField("offset", offset).
			Error("failed pushing")

		return nil, errors.New("failed pushing")
	}

	logrus.
		WithField("component", "target-kafka").
		WithField("partition", partition).
		WithField("offset", offset).
		Debug("")

	return nil, nil
}
