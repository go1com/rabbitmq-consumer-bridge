package rabbitmq_consumer_bridge

import (
	"encoding/json"
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"github.com/tidwall/gjson"
)

type Message struct {
	Subject string                 `json:"subject"`
	Message map[string]interface{} `json:"message"`
	Context map[string]interface{} `json:"context"`
}

type Messages struct {
	Messages []Message `json:"messages"`
}

type PipelineRabbitMq struct {
	cnf        *RabbitMqTargetConfig
	Connection *amqp.Connection
	Channel    *amqp.Channel
}

func NewRabbitMqPipeline(cnf *RabbitMqTargetConfig) (PipeLine, error) {
	p := PipelineRabbitMq{
		cnf: cnf,
	}

	conn, err := connection(cnf.URL)
	if nil != err {
		return p, err
	}

	p.Connection = conn
	p.Channel = channel(conn, cnf.Kind, cnf.Exchange)

	return p, nil
}

func (p PipelineRabbitMq) invoke(data []byte) bool {
	dataType := gjson.GetBytes(data, "type").String()
	messages := Messages{}

	switch dataType {
	case "publish.messages":
		if err := json.Unmarshal(data, &messages); err != nil {
			logrus.
				WithError(err).
				WithField("component", "pipeline-rabbitmq").
				Error("failed to parse messages")

			return false
		}
		break

	case "publish.message":
		message := Message{}
		if err := json.Unmarshal(data, &message); err != nil {
			logrus.
				WithError(err).
				WithField("component", "pipeline-rabbitmq").
				Error("failed to parse message")

			return false
		}
		messages.Messages = append(messages.Messages, message)
		break
	}

	for _,m := range messages.Messages {
		body, _ := json.Marshal(m.Message)
		msg := amqp.Publishing{
			Body:    body,
			Headers: m.Context,
		}

		err := p.Channel.Publish(p.cnf.Exchange, m.Subject, false, false, msg)
		if err != nil {
			logrus.
				WithError(err).
				WithField("component", "pipeline-rabbitmq").
				WithField("subject", m.Subject).
				Error("failed to push message to rabbmitmq")
			return false
		}
	}

	return true
}
