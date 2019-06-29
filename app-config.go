package rabbitmq_consumer_bridge

import (
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"github.com/tidwall/gjson"

	"gopkg.in/yaml.v2"
)

type AppConfig struct {
	Debug          bool                                 `yaml:"debug"`
	Prefix         string                               `yaml:"prefix"`
	HttpClient     *HttpClientConfig                    `yaml:"http-client"`
	Services       []ServiceConfig                      `yaml:"services"`
	RetryIntervals []time.Duration                      `yaml:"retry-intervals"`
	DeadLetter     *DeadLetter                          `yaml:"dead-letter"`
	RabbitMq       *map[string]RabbitMqConnectionOption `yaml:"rabbitmq"`
	Lambda         *LambdaConfig                        `yaml:"lambda"`
	Kafka          *map[string]KafkaConnectionOptions   `yaml:"kafka"`
	Prometheus     *PrometheusConfig                    `yaml:"prometheus"`
}

func (cnf *AppConfig) NextIdleTime(key int) (time.Duration, int) {
	idleTime := cnf.RetryIntervals[key]

	if len(cnf.RetryIntervals) == key+1 {
		return idleTime, 0
	}

	return idleTime, key + 1
}

func NewAppConfig(yamlRaw []byte) (*AppConfig, error) {
	yamlString := os.ExpandEnv(string(yamlRaw))

	cnf := &AppConfig{}
	err := yaml.Unmarshal([]byte(yamlString), cnf)
	if err != nil {
		return nil, err
	}

	if "" == cnf.Prefix {
		cnf.Prefix = Env("GROUP_PREFIX", "group")
	}

	if 0 == len(cnf.RetryIntervals) {
		cnf.RetryIntervals = []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, 45 * time.Second, 60 * time.Second}
	}

	// default HttpClient options
	// ---------------------
	if nil == cnf.HttpClient {
		cnf.HttpClient = &HttpClientConfig{}
	}

	cnf.HttpClient.onParse()

	// define lambda invoker
	// ---------------------
	if nil != cnf.Lambda {
		cnf.Lambda.OnAppStart(cnf)
	}

	// default ServiceConfig options
	// ---------------------
	for i, _ := range cnf.Services {
		cnf.Services[i].onParse(cnf)
	}

	return cnf, nil
}

func (cnf *AppConfig) enabledMessageSplitting() bool {
	for _, service := range cnf.Services {
		if service.Split > 0 {
			return true
		}
	}
	return false
}

type RouteConfig struct {
	Name      string     `yaml:"name"`
	Condition *Condition `yaml:"condition"`
}

type TargetConfig struct {
	Type     string                `yaml:"type"`
	RabbitMq *RabbitMqTargetConfig `yaml:"rabbitmq"`
	Kafka    *KafkaServiceConfig   `yaml:"kafka"`
	Process  ProcessTargetConfig   `yaml:"process"`
}

type Conditions []Condition
type Condition struct {
	// operators
	Type string        `yaml:"type"`
	And  *AndCondition `yaml:"and"`
	Or   *OrCondition  `yaml:"or"`
	Not  *Condition    `yaml:"not"`

	// functions
	Text  *TextFunction  `yaml:"text"`
	GJson *GJsonFunction `yaml:"gjson"`
}

func (c *Condition) validate(m *amqp.Delivery) bool {
	switch c.Type {
	case "and":
		return c.And.validate(m)

	case "or":
		return c.Or.validate(m)

	case "not":
		return !c.Not.validate(m)

	case "text":
		return c.Text.valdiate(m)

	case "gjson":
		return c.GJson.validate(m)
	}

	return false
}

type AndCondition Conditions

func (conditions AndCondition) validate(m *amqp.Delivery) bool {
	for _, c := range conditions {
		if !c.validate(m) {
			return false
		}
	}
	return true
}

type OrCondition Conditions

func (conditions OrCondition) validate(m *amqp.Delivery) bool {
	for _, c := range conditions {
		if c.validate(m) {
			return true
		}
	}
	return false
}

type TextFunction struct {
	Part     string `yaml:"part"` // body, headers.X
	Operator string `yaml:"op"`   // `string` operators: match, startWith, endWith, contain
	Argument string `yaml:"arg"`
}

func (c *TextFunction) valdiate(m *amqp.Delivery) bool {
	subject := string(part(m, c.Part))

	switch c.Operator {
	case "match":
		return c.Argument == subject

	case "startsWith":
		return strings.HasPrefix(subject, c.Argument)

	case "endsWith":
		return strings.HasSuffix(subject, c.Argument)

	case "contains":
		return strings.Contains(subject, c.Argument)
	}

	return false
}

type GJsonFunction struct {
	Part  string `yaml:"part"` // body, headers.X
	Query string `yaml:"query"`
	// `string` operators: match, startWith, endWith, contain
	// `number` operators: equal, greaterThan, greaterThanOrEqual, lessThan, lessThanOrEquals
	Operator string `yaml:"op"`
	Argument string `yaml:"arg"`
}

func (c *GJsonFunction) argInt() int64 {
	arg, err := strconv.ParseInt(c.Argument, 10, 64)
	if err != nil {
		return 0
	}

	return arg
}

func (c *GJsonFunction) validate(m *amqp.Delivery) bool {
	subject := part(m, c.Part)
	result := gjson.GetBytes(subject, c.Query)

	switch c.Operator {
	// string operators
	// ---------------------
	case "match":
		return c.Argument == result.String()

	case "startsWith":
		return strings.HasPrefix(result.String(), c.Argument)

	case "endsWith":
		return strings.HasSuffix(result.String(), c.Argument)

	case "contains":
		return strings.Contains(result.String(), c.Argument)

	// number operators
	// ---------------------
	case "equal":
		return result.Int() == c.argInt()

	case "greaterThan":
		return result.Int() > c.argInt()

	case "greaterThanOrEequal":
		return result.Int() >= c.argInt()

	case "lessThan":
		return result.Int() < c.argInt()

	case "lessThanOrEequal":
		return result.Int() <= c.argInt()
	}

	return false
}

// ***************************************************************
// Utilitily for creating new AppConfig
// ***************************************************************
func AppConfigFromEnv() *AppConfig {
	configPath := Env("CONSUMER_CONFIG_YAML_FILE", "")

	return AppConfigFromYamlFile(configPath)
}

func AppConfigFromYamlFile(path string) *AppConfig {
	var err error
	var yamlBytes []byte

	if "" != path {
		yamlBytes, err = ioutil.ReadFile(path)
		if err != nil {
			logrus.WithError(err).Panic("failed reading config file")
		}
	} else {
		input := Env("CONSUMER_CONFIG_YAML", "")
		yamlBytes = []byte(input)
	}

	return AppConfigFromYamlBytes(yamlBytes)
}

func AppConfigFromYamlBytes(yamlBytes []byte) *AppConfig {
	cnf, err := NewAppConfig(yamlBytes)
	if err != nil {
		logrus.WithError(err).Panic("failed parsing yaml config")
	}

	if 0 == len(cnf.Services) {
		logrus.Panic("bad config, no service found")
	}

	for _, service := range cnf.Services {
		if 0 == len(service.Routes) {
			logrus.
				WithField("service", service.Name).
				Panic("bad config, no route found")
		}
	}

	return cnf
}
