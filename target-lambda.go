package rabbitmq_consumer_bridge

import (
	"encoding/json"
	"errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

type LambdaConfig struct {
	AuthKey        string `yaml:"auth-key"`
	AuthSecret     string `yaml:"auth-secret"`
	Region         string `yaml:"region"`
	Version        string `yaml:"version"`
	InvocationType string `yaml:"invocation-type"`

	invoker *lambda.Lambda
}

func (o *LambdaConfig) OnAppStart(cnf *AppConfig) {
	awsConfig := aws.NewConfig().
		WithRegion(cnf.Lambda.Region).
		WithCredentials(credentials.NewStaticCredentials(cnf.Lambda.AuthKey, cnf.Lambda.AuthSecret, ""))
	sess, err := session.NewSession(awsConfig)
	if err != nil {
		logrus.WithError(err).Panic("failed to create aws session")
	}

	cnf.Lambda.invoker = lambda.New(sess)
}

type LambdaTarget struct {
	cnf          *LambdaConfig
	functionName string
}

func NewLambdaTarget(functionName string, cnf *LambdaConfig) (Target, error) {
	t := &LambdaTarget{
		cnf:          cnf,
		functionName: functionName,
	}

	return t, nil
}

func (t *LambdaTarget) start() error { return nil }

func (t *LambdaTarget) terminate() error { return nil }

func (t *LambdaTarget) handle(m *amqp.Delivery) ([]byte, error) {
	payload, _ := json.Marshal(
		map[string]interface{}{
			"method":  "POST",
			"url":     "/consume?jwt=" + RootJwt,
			"headers": map[string]string{"Content-Type": "application/json"},
			"body": map[string]interface{}{
				"routingKey": m.RoutingKey,
				"body":       m.Body,
				"context":    m.Headers,
			},
		},
	)

	input := &lambda.InvokeInput{
		FunctionName:   &t.functionName,
		InvocationType: &t.cnf.InvocationType,
		Payload:        payload,
	}

	_, err := t.cnf.invoker.Invoke(input)
	if nil == err {
		return nil, nil
	}

	logrus.
		WithError(err).
		WithField("component", "target-lambda").
		Error("failed to invoke lambda function")

	return nil, errors.New("failed to invoke lambda function")
}
