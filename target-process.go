package rabbitmq_consumer_bridge

import (
	"bytes"
	"context"
	"errors"
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"os/exec"
	"strings"
	"time"
)

type ProcessTarget struct {
	serviceName string
	cnf         ProcessTargetConfig
}

type ProcessTargetConfig struct {
	Cmd     string        `yaml:"cmd"`
	Timeout time.Duration `yaml:"timeout"`
}

func NewProcessTarget(serviceName string, cnf ProcessTargetConfig) (Target, error) {
	t := &ProcessTarget{
		serviceName: serviceName,
		cnf:         cnf,
	}

	if t.cnf.Timeout == 0 {
		t.cnf.Timeout = 10*time.Second
	}

	return t, nil
}

func (t *ProcessTarget) start() error { return nil }

func (t *ProcessTarget) terminate() error { return nil }

func (t *ProcessTarget) handle(m *amqp.Delivery) ([]byte, error) {
	var (
		stdOut bytes.Buffer
		stdErr bytes.Buffer
	)

	ctx, cancel := context.WithTimeout(context.Background(), t.cnf.Timeout)
	defer cancel() // The cancel should be deferred so resources are cleaned up

	// Parse process config to command format
	// Ex: `php /tmp/fn.php` wil become
	// 	 name: php
	// 	 args: [/tmp/fn.php]
	process := strings.Fields(t.cnf.Cmd)
	name := process[0]
	args := process[1:]
	args = append(args, m.RoutingKey)
	args = append(args, string(m.Body))

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdOut
	cmd.Stderr = &stdErr
	err := cmd.Run()

	if 0 != stdErr.Len() {
		logrus.
			WithError(err).
			WithField("msg.routingKey", m.RoutingKey).
			WithField("msg.body", string(m.Body)).
			WithField("cmd", t.cnf.Cmd).
			WithField("error", stdErr.String()).
			Errorln("stderr")


		return nil, errors.New("failed to execute the process")
	}

	if ctx.Err() == context.DeadlineExceeded {
		logrus.
			WithError(ctx.Err()).
			Errorln("target-process execution timed out")

		return nil, ctx.Err()
	}

	output := string(stdOut.Bytes())
	if len(output) > 0 {
		return stdOut.Bytes(), nil
	}

	return nil, nil
}
