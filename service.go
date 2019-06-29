package rabbitmq_consumer_bridge

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/go-errors/errors"
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

type Service struct {
	cnf      *ServiceConfig
	con      *amqp.Connection
	ch       *amqp.Channel
	chGroup  *amqp.Channel
	target   Target
	pipeline PipeLine
	retryKey int
	worker   int
}

type ServiceConfig struct {
	Split           int             `yaml:"split"`
	ExcludeMonolith bool            `yaml:"exclude-monolith"`
	Name            string          `yaml:"name"`
	Queue           string          `yaml:"queue"`
	Routes          []RouteConfig   `yaml:"routes"`
	Target          *TargetConfig   `yaml:"target"`
	Pipeline        *PipelineConfig `yaml:"pipeline"`
	DeadLetter      *DeadLetter     `yaml:"dead-letter"`
	Worker          int             `yaml:"worker"`
}

func (s *ServiceConfig) onParse(cnf *AppConfig) {
	if s.Worker < 1 {
		s.Worker = 1
	}

	if "" == s.Queue {
		s.Queue = s.Name
	}

	s.Name = strings.Split(s.Name, "__")[0]
	s.Queue = cnf.Prefix + ":" + s.Queue

	if nil != cnf.DeadLetter {
		if nil == s.DeadLetter {
			s.DeadLetter = cnf.DeadLetter
		}
	}
}

func (c *ServiceConfig) routingKeys() []string {
	routingKeys := []string{}
	for _, route := range c.Routes {
		routingKeys = append(routingKeys, route.Name)
	}
	return routingKeys
}

func NewService(appConfig *AppConfig, serviceConfig *ServiceConfig, worker int) (*Service, error) {
	target, err := NewTarget(serviceConfig)
	if nil != err {
		return nil, err
	}

	if err = target.start(); nil != err {
		return nil, err
	}

	if nil == appConfig.RabbitMq {
		return nil, errors.New("no rabbitmq connection configured")
	}

	conConfig := *appConfig.RabbitMq
	c := &Service{
		cnf:      serviceConfig,
		con:      conConfig["default"].Connection(),
		ch:       channel(conConfig["default"].Connection(), "topic", "events"),
		target:   target,
		retryKey: 0,
		worker:   worker,
	}

	if serviceConfig.Pipeline != nil {
		pipeline, err := NewPipeLine(serviceConfig)
		if nil != err {
			return nil, err
		}
		c.pipeline = pipeline
	}

	if serviceConfig.Split > 0 {
		c.chGroup = channel(c.con, "direct", "consumer_group")
	}

	return c, nil
}

func (c *Service) log(err error) *logrus.Entry {
	log := logrus.
		WithField("component", "queue").
		WithField("queue", c.cnf.Queue).
		WithField("service", c.cnf.Name)

	if nil != err {
		log.WithError(err)
	}

	return log
}

func (c *Service) start(ctx context.Context, terminate chan bool) {
	defer func() {
		c.log(nil).Infoln("terminating")
		c.target.terminate()
		c.ch.Close()

		app.groupProcess.Done()
	}()

	app.groupProcess.Add(1)

	var handler = c.handler(c.ch)
	if c.cnf.Split > 0 {
		handler = c.dispatchToServiceWorker(c.ch)
		for i := 0; i < c.cnf.Split; i++ {
			go c.startServiceWorker(i, ctx, terminate)
		}
	}

	loop(
		terminate,
		stream(c.ch, "events", c.cnf.Queue, c.cnf.routingKeys()),
		c.cnf,
		handler,
		func(uuid string, m amqp.Delivery, failedValidation bool) {
			c.ch.Nack(m.DeliveryTag, true, false)

			if failedValidation {
				counter, _ := promFilteredMessageCounter.GetMetricWithLabelValues(c.cnf.Queue, c.cnf.Name, m.RoutingKey)
				counter.Desc()

				promFilteredMessageCounter.
					WithLabelValues(c.cnf.Queue, c.cnf.Name, m.RoutingKey).
					Inc()
			}
		},
	)
}

func (c *Service) handler(ch *amqp.Channel) func(m *amqp.Delivery) {
	return func(m *amqp.Delivery) {
		defer func() {
			if err := recover(); nil != err {
				c.log(nil).
					WithField("msg.routingKey", m.RoutingKey).
					WithField("error.trace", errors.New(err).ErrorStack()).
					Errorf("recovered from panic: %s", err)
			}
		}()

		m.Headers["X-QUEUE"] = c.cnf.Queue

		if xRoutingKey, ok := m.Headers["X-ROUTING-KEY"]; ok {
			m.Headers["X-ORIGINAL-ROUTING-KEY"] = m.RoutingKey
			m.RoutingKey = xRoutingKey.(string)

			delete(m.Headers, "X-ROUTING-KEY")
		}

		if nil != c.cnf.DeadLetter {
			if !m.Redelivered {
				c.cnf.DeadLetter.start()
			}
		}

		retry(
			func() bool {
				isDeathMessage := nil != c.cnf.DeadLetter && c.cnf.DeadLetter.HandleFailure(c.cnf, m)
				if isDeathMessage {
					ch.Nack(m.DeliveryTag, false, false)
					return true
				}

				response, err := c.target.handle(m)
				if err != nil {
					c.log(err).
						WithField("msg.routingKey", m.RoutingKey).
						Errorf("failed execute the target")

					return false
				}

				if response != nil && c.pipeline != nil && !c.pipeline.invoke(response) {
					c.log(err).
						WithField("msg.routingKey", m.RoutingKey).
						Errorf("failed execute the pipeline")

					return false
				}

				ch.Ack(m.DeliveryTag, true)
				return true
			},
			func() {
				var retryInterval time.Duration
				retryInterval, c.retryKey = app.config.NextIdleTime(c.retryKey)

				promFailureMessageCounter.WithLabelValues(c.cnf.Queue, c.cnf.Name, m.RoutingKey).Inc()
				promRetryMessageCounter.WithLabelValues(c.cnf.Queue, c.cnf.Name, m.RoutingKey).Inc()

				c.log(nil).
					WithField("msg.routingKey", m.RoutingKey).
					Errorf("failed handling m, retry in: %s", retryInterval)

				ch.Nack(m.DeliveryTag, false, true)
				time.Sleep(retryInterval)
			},
			func() {
				promSuccessMessageCounter.WithLabelValues(c.cnf.Queue, c.cnf.Name, m.RoutingKey).Inc()
			},
		)
	}
}

func (c *Service) dispatchToServiceWorker(ch *amqp.Channel) func(m *amqp.Delivery) {
	return func(m *amqp.Delivery) {
		idFromPayload := gjson.GetBytes(m.Body, "id").Int()
		destinationId := idFromPayload % int64(c.cnf.Split)
		routingKey := c.cnf.Queue + ":" + strconv.FormatInt(int64(destinationId), 10)

		delete(m.Headers, "portal-name")
		delete(m.Headers, "entity-type")
		m.Headers["X-SERVICE"] = c.cnf.Name
		m.Headers["X-ROUTING-KEY"] = m.RoutingKey

		err := c.chGroup.Publish("consumer_group", routingKey, false, false, convert(m))
		if nil != err {
			c.log(err).
				WithField("routingKey", routingKey).
				Error("[group.dispatch] failed dispatching m to sub-consumer")
		} else {
			c.log(nil).
				WithField("routingKey", routingKey).
				Info("[group.dispatch] message is dispatched to sub-consumer")
			ch.Ack(m.DeliveryTag, true)
		}
	}
}

func (c *Service) startServiceWorker(id int, ctx context.Context, terminate chan bool) {
	var (
		queueName = c.cnf.Queue + ":" + strconv.Itoa(id)
		ch        = channel(c.con, "direct", "consumer_group")
	)

	defer func() {
		logrus.
			WithField("component", "consumer.group").
			WithField("queue", queueName).
			Info("terminating")

		app.groupProcess.Done()
		ch.Close()
	}()

	app.groupProcess.Add(1)
	loop(
		terminate,
		stream(ch, "consumer_group", queueName, []string{queueName}),
		nil,
		c.handler(ch),
		nil,
	)
}
