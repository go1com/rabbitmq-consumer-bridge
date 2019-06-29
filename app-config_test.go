package rabbitmq_consumer_bridge

import (
	"path"
	"runtime"
	"testing"
	"time"

	"github.com/streadway/amqp"
)

func TestConfigPrefix(t *testing.T) {
	_, cnfFile, _, _ := runtime.Caller(0)
	cnf := AppConfigFromYamlFile(path.Dir(cnfFile) + "/fixtures/target-http-config.yaml")

	if "qa" != cnf.Prefix {
		t.FailNow()
	}
}

func TestRabbitMqConfig(t *testing.T) {
	_, cnfFile, _, _ := runtime.Caller(0)
	cnf := AppConfigFromYamlFile(path.Dir(cnfFile) + "/fixtures/target-http-config.yaml")

	queueCnf := *cnf.RabbitMq

	if Env("QUEUE_URL", "") != queueCnf["default"].Url {
		t.FailNow()
	}
}

func TestGjsonValidateBody(t *testing.T) {
	m := &amqp.Delivery{
		Body:    []byte(`{"type": "event", "title": "Awesome event somewhere on earth"}`),
		Headers: amqp.Table{"X-VERSION": "v1.0.0"},
	}

	// msg.body.type == event
	f := GJsonFunction{Query: `type`, Part: "body", Operator: "match", Argument: "event"}
	if !f.validate(m) {
		t.FailNow()
	}

	c := Condition{Type: "gjson", GJson: &f}
	if !c.validate(m) {
		t.FailNow()
	}
}

func TestTextFunction(t *testing.T) {
	m := &amqp.Delivery{
		Body:    []byte(`{"type": "event", "title": "Awesome event somewhere on earth"}`),
		Headers: amqp.Table{"X-VERSION": "v1.0.0"},
	}

	truth := TextFunction{Part: "headers.X-VERSION", Operator: "match", Argument: "v1.0.0"}
	if !truth.valdiate(m) {
		t.FailNow()
	}

	untruth := TextFunction{Part: "headers.X-VERSION", Operator: "match", Argument: "v2.0.0"}
	if untruth.valdiate(m) {
		t.FailNow()
	}
}

func TestNotCondition(t *testing.T) {
	m := &amqp.Delivery{
		Body:    []byte(`{"type": "event", "title": "Awesome event somewhere on earth"}`),
		Headers: amqp.Table{"X-VERSION": "v1.0.0"},
	}

	// msg.type != course
	c := Condition{
		Type: "not",
		Not: &Condition{
			Type:  "gjson",
			GJson: &GJsonFunction{Query: `type`, Part: "body", Operator: "match", Argument: "course"},
		},
	}

	if !c.validate(m) {
		t.FailNow()
	}
}

func TestAndCondition(t *testing.T) {
	m := &amqp.Delivery{
		Body:    []byte(`{"type": "event", "title": "Awesome event somewhere on earth"}`),
		Headers: amqp.Table{"X-VERSION": "v1.0.0"},
	}

	/* true */ c1 := Condition{Type: "gjson", GJson: &GJsonFunction{Query: `type`, Part: "body", Operator: "match", Argument: "event"}}
	/* true */ c2 := Condition{Type: "text", Text: &TextFunction{Part: "headers.X-VERSION", Operator: "match", Argument: "v1.0.0"}}

	// msg.body.type == event && msg.headers[X-VERSION] == v1.0.0
	c12 := AndCondition{c1, c2}
	if !c12.validate(m) {
		t.FailNow()
	}
}

func TestOrCondition(t *testing.T) {
	m := &amqp.Delivery{
		Body:    []byte(`{"type": "event", "title": "Awesome event somewhere on earth"}`),
		Headers: amqp.Table{"X-VERSION": "v1.0.0"},
	}

	/*  true */ c1 := Condition{Type: "gjson", GJson: &GJsonFunction{Query: `type`, Part: "body", Operator: "match", Argument: "event"}}
	/* false */ c2 := Condition{Type: "gjson", GJson: &GJsonFunction{Query: `type`, Part: "body", Operator: "match", Argument: "course"}}
	/*  true */ c3 := Condition{Type: "text", Text: &TextFunction{Part: "headers.X-VERSION", Operator: "match", Argument: "v1.0.0"}}
	/* false */ c4 := Condition{Type: "text", Text: &TextFunction{Part: "headers.X-VERSION", Operator: "match", Argument: "v2.0.0"}}

	if !c1.validate(m) || c2.validate(m) || !c3.validate(m) || c4.validate(m) {
		t.FailNow()
	}

	// true || false => true
	c12 := OrCondition{c1, c2}
	if !c12.validate(m) {
		t.FailNow()
	}

	// true || true => true
	c13 := OrCondition{c1, c3}
	if !c13.validate(m) {
		t.FailNow()
	}

	// false || false => false
	c24 := OrCondition{c2, c4}
	if c24.validate(m) {
		t.FailNow()
	}
}

func TestDeadLetterAppConfig(t *testing.T) {
	app = NewApp(make(chan bool))
	cnf, _ := NewAppConfig([]byte(`
dead-letter: &ref-dead-letter
  condition:
    attempts: 20
    timeout: "15m"
  target: "http"
  http:
    method: "POST"
    url:    "http://dead.letter/webhook"
    body:   'payload={"text": %dead-letter%}'
services:
- name: "my-service"
  routes:
    - name: "some-event"
- name: "my-other-service"
  routes:
    - name: "some-other-event"
  dead-letter:
    <<: *ref-dead-letter
    condition:
      attempts: 30
      timeout: "20m"
`))
	if 20 != cnf.Services[0].DeadLetter.Condition.Attempts {
		t.FailNow()
	}

	if 30 != cnf.Services[1].DeadLetter.Condition.Attempts {
		t.FailNow()
	}

	if 15*time.Minute != *cnf.Services[0].DeadLetter.Condition.Timeout {
		t.FailNow()
	}

	if 20*time.Minute != *cnf.Services[1].DeadLetter.Condition.Timeout {
		t.FailNow()
	}
}

func TestDeadLetterServiceConfig(t *testing.T) {
	app = NewApp(make(chan bool))
	cnf, _ := NewAppConfig([]byte(`
services:
- name: "my-service"
  routes:
    - name: "some-event"
  dead-letter:
    condition:
      attempts: 20
      timeout: "15m"
    target: "http"
    http:
      method: "POST"
      url:    "http://dead.letter/webhook"
      body:   'payload={"text": %dead-letter%}'
`))
	if 20 != cnf.Services[0].DeadLetter.Condition.Attempts {
		t.FailNow()
	}

	if 15*time.Minute != *cnf.Services[0].DeadLetter.Condition.Timeout {
		t.FailNow()
	}

	if "http" != cnf.Services[0].DeadLetter.Target {
		t.FailNow()
	}

	if "POST" != cnf.Services[0].DeadLetter.Http.Method {
		t.FailNow()
	}

	if "http://dead.letter/webhook" != cnf.Services[0].DeadLetter.Http.Url {
		t.FailNow()
	}

	if `payload={"text": %dead-letter%}` != cnf.Services[0].DeadLetter.Http.Body {
		t.FailNow()
	}
}
