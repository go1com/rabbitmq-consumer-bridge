package rabbitmq_consumer_bridge

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-errors/errors"
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

type DeadLetter struct {
	Condition DeadLetterCondition   `yaml:"condition"`
	Target    string                `yaml:"target"`
	Http      *DeadLetterHttpTarget `yaml:"http"`

	started  time.Time
	attempts int
}

type DeadLetterCondition struct {
	Attempts int            `yaml:"attempts"`
	Timeout  *time.Duration `yaml:"timeout"`
}

type DeadLetterHttpTarget struct {
	Method string `yaml:"method"`
	Url    string `yaml:"url"`
	Body   string `yaml:"body"`
}

func (dl *DeadLetter) start() {
	dl.started = time.Now()
	dl.attempts = 0
}

func (dl *DeadLetter) HandleFailure(service *ServiceConfig, m *amqp.Delivery) bool {
	dl.attempts += 1
	deliver := true

	if nil != dl.Condition.Timeout {
		if time.Since(dl.started) < *dl.Condition.Timeout {
			deliver = false
		}
	}

	if dl.Condition.Attempts > 0 {
		if dl.attempts < dl.Condition.Attempts {
			deliver = false
		}
	}

	if deliver {
		return dl.deliver(service, m)
	}

	return false
}

func (dl *DeadLetter) deliver(service *ServiceConfig, m *amqp.Delivery) bool {
	switch dl.Target {
	case "http":
		return dl.Http.deliver(service, m)
	}

	return false
}

func (t *DeadLetterHttpTarget) deliver(service *ServiceConfig, m *amqp.Delivery) bool {
	logrus.
		WithField("service", service.Name).
		WithField("queue", service.Queue).
		WithField("m.routingKey", m.RoutingKey).
		WithField("m.body", string(m.Body)).
		WithField("t.method", t.Method).
		WithField("t.url", t.Url).
		WithField("t.body", t.Body).
		Info("deliver dead-letter")

	defer func() {
		if err := recover(); nil != err {
			logrus.
				WithField("error.trace", errors.New(err).ErrorStack()).
				WithField("component", "dead-letter").
				Errorf("recovered from panic: %s", err)
		}
	}()

	body := "service: %s queue: %s routingKey: %s body: %s"
	body = fmt.Sprintf(body, service.Name, service.Queue, m.RoutingKey, string(m.Body))
	body = strings.Replace(t.Body, "%dead-letter%", strconv.Quote(body), -1)
	req, _ := http.NewRequest(t.Method, t.Url, bytes.NewBufferString(body))
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	res, _ := app.config.HttpClient.Get().Do(req)

	if 200 != res.StatusCode {
		logrus.
			WithField("component", "dead-letter").
			WithField("requestMethod", t.Method).
			WithField("requestUrl", t.Url).
			WithField("requestBody", body).
			WithField("responseStatusCode", res.StatusCode).
			Panic("response")

		return false
	}

	logrus.
		WithField("component", "dead-letter").
		WithField("requestMethod", t.Method).
		WithField("requestUrl", t.Url).
		WithField("requestBody", body).
		WithField("responseStatusCode", res.StatusCode).
		Info("response")

	io.Copy(ioutil.Discard, res.Body)
	res.Body.Close()

	return true
}
