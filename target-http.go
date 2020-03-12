package rabbitmq_consumer_bridge

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

const (
	RootJwt = "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJvYmplY3QiOnsidHlwZSI6InVzZXIiLCJjb250ZW50Ijp7ImlkIjoxLCJwcm9maWxlX2lkIjoxLCJyb2xlcyI6WyJBZG1pbiBvbiAjQWNjb3VudHMiXSwibWFpbCI6IjFAMS4xIn19fQ.YwGrlnegpd_57ek0vew5ixBfzhxiepc5ODVwPva9egs"
)

type HttpTarget struct {
	client  *http.Client
	queue   string
	service string
	split   int
}

type HttpClientConfig struct {
	ServiceUrlPattern   string        `yaml:"service-url-pattern"`
	MaxIdleConns        int           `yaml:"max-idle-connections"`
	MaxIdleConnsPerHost int           `yaml:"max-idle-connections-per-host"`
	IdleConnTimeout     time.Duration `yaml:"idle-connection-timeout"`
	TimeoutConnection   time.Duration `yaml:"timeout-connection"`
	TimeoutRequest      time.Duration `yaml:"timeout-request"`

	client *http.Client
	mu     sync.Mutex
}

func (c *HttpClientConfig) onParse() {
	c.mu = sync.Mutex{}

	if 0 == c.MaxIdleConns {
		c.MaxIdleConns = 100
	}

	if 0 == c.MaxIdleConnsPerHost {
		c.MaxIdleConnsPerHost = 20
	}

	if 0 == c.IdleConnTimeout {
		c.IdleConnTimeout = 90 * time.Second
	}

	if 0 == c.TimeoutConnection {
		c.TimeoutConnection = 2 * time.Second
	}

	if 0 == c.TimeoutRequest {
		c.TimeoutRequest = 30 * time.Second
	}
}

func (c *HttpClientConfig) Get() *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()

	if nil == c.client {
		c.client = &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        c.MaxIdleConns,
				MaxIdleConnsPerHost: c.MaxIdleConnsPerHost,
				IdleConnTimeout:     c.IdleConnTimeout,
				Dial: func(network, addr string) (net.Conn, error) {
					return net.DialTimeout(network, addr, c.TimeoutConnection)
				},
			},
			Timeout: c.TimeoutRequest,
		}
	}

	return c.client
}

func (c *HttpClientConfig) SetServiceUrlPattern(pattern string) {
	c.ServiceUrlPattern = pattern
}

func NewHttpTarget(cnf *ServiceConfig, client *http.Client) (Target, error) {
	return &HttpTarget{
		client:  client,
		queue:   cnf.Queue,
		service: cnf.Name,
		split:   cnf.Split,
	}, nil
}

func (t *HttpTarget) log(err error) *logrus.Entry {
	log := logrus.
		WithField("component", "queue").
		WithField("queue", t.queue).
		WithField("service", t.service)

	if nil != err {
		log.WithError(err)
	}

	return log
}

func (t *HttpTarget) start() error     { return nil }
func (t *HttpTarget) terminate() error { return nil }
func (t *HttpTarget) handle(m *amqp.Delivery) ([]byte, error) {
	if !push(t.queue, t.service, m) {
		return nil, errors.New("failed to push")
	}

	return nil, nil
}

// ***************************************************************
// TODO: legacy stuff to be refactored
// ***************************************************************

type Payload struct {
	RoutingKey string     `json:"routingKey"`
	Body       string     `json:"body"`
	Context    amqp.Table `json:"context"`
}

func (m *Payload) push(client *http.Client, url string) error {
	bodyReader, _ := json.Marshal(m)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(bodyReader))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+RootJwt)

	if nil != app {
		req.Header.Set("User-Agent", "go1.consumer/"+app.version)
	}

	if requestId, ok := m.Context["request_id"]; ok {
		req.Header.Add("X-Request-Id", requestId.(string))
	}

	for k, v := range m.Context {
		add := strings.HasPrefix(k, "x-datadog-") ||
			strings.HasPrefix(k, "x-datadog-") ||
			strings.HasPrefix(k, "ot-baggage-")

		if add {
			stringValue, ok := v.(string)
			if ok {
				req.Header.Set(k, stringValue)
				delete(m.Context, k)
			}
		}
	}

	res, err := client.Do(req)
	if nil != err {
		return err
	}

	io.Copy(ioutil.Discard, res.Body)
	res.Body.Close()

	if app.handleResponseCode(res.StatusCode) {
		return nil
	}

	return fmt.Errorf("service response status is not 204")
}

func (app *Application) push(service string, m *amqp.Delivery) bool {
	url := getPath(service, app.env, app.config.HttpClient.ServiceUrlPattern)

	maxBodySize := 2 * 1024 * 1024
	if binary.Size(m.Body) > maxBodySize {
		logrus.
			WithField("service", service).
			WithField("service.url", url).
			WithField("message.routingKey", m.RoutingKey).
			Error("service failed handling because body too long")

		return true
	}

	payload := Payload{
		RoutingKey: m.RoutingKey,
		Body:       string(m.Body),
		Context:    m.Headers,
	}

	err := payload.push(app.config.HttpClient.Get(), url)
	if err == nil {
		return true
	}

	context, _ := json.Marshal(&m.Headers)
	logrus.
		WithError(err).
		WithField("service", service).
		WithField("service.url", url).
		WithField("message.routingKey", m.RoutingKey).
		WithField("message.context", string(context)).
		WithField("message.body", string(m.Body)).
		Error("service failed handling")

	return false
}

// Push to microservice with request structure depends on message.
// @TODO: We should unserstand when the message failed handling.
func (app *Application) pushDynamic(service string, m *amqp.Delivery) bool {
	var payload struct {
		Method  string            `json:"method"`
		Url     string            `json:"url"`
		Query   string            `json:"query"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}

	err := json.Unmarshal(m.Body, &payload)
	if nil != err {
		logrus.
			WithError(err).
			WithField("component", "target.push.default").
			WithField("service", service).
			WithField("message.routingKey", m.RoutingKey).
			WithField("message.body", string(m.Body)).
			Error("failed to parse message body")

		return false
	}

	req, _ := http.NewRequest(payload.Method, payload.Url, nil)

	for key, value := range payload.Headers {
		req.Header.Add(key, value)
	}

	if "GET" != payload.Method {
		if "OPTIONS" != payload.Method {
			bodyReader := strings.NewReader(payload.Body)
			req.Body = ioutil.NopCloser(bodyReader)
		}
	}

	res, err := app.config.HttpClient.Get().Do(req)
	if nil != err {
		context, _ := json.Marshal(&m.Headers)

		logrus.
			WithError(err).
			WithField("component", "target.push.default").
			WithField("service", service).
			WithField("message.routingKey", m.RoutingKey).
			WithField("message.context", string(context)).
			WithField("message.body", string(m.Body)).
			Error("failed request")
	}

	io.Copy(ioutil.Discard, res.Body)
	res.Body.Close()

	return app.handleResponseCode(res.StatusCode)
}

func (app *Application) handleResponseCode(statusCode int) bool {
	return statusCode == http.StatusNoContent ||
		statusCode == http.StatusOK ||
		statusCode == http.StatusBadRequest ||
		statusCode == http.StatusRequestEntityTooLarge ||
		statusCode == http.StatusForbidden
}
