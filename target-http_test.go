package rabbitmq_consumer_bridge

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

type serviceLog struct {
	items [][]byte
}

func (sl *serviceLog) reset() {
	sl.items = [][]byte{}
}

func (sl *serviceLog) add(item []byte) {
	sl.items = append(sl.items, item)
}

func getModels(forever chan bool) (*Application, *amqp.Connection, *httptest.Server, *serviceLog) {
	_, currentFileName, _, _ := runtime.Caller(0)
	filePath := path.Dir(currentFileName) + "/fixtures/target-http-config.yaml"

	initialized = 0
	app := NewApp(forever)
	cnf := AppConfigFromYamlFile(filePath)
	ctx := context.Background()
	queueCnf := *cnf.RabbitMq
	queueUrl := queueCnf["default"].Url
	queueCon, _ := connection(queueUrl)
	sl := &serviceLog{items: [][]byte{}}

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				body, _ := ioutil.ReadAll(r.Body)
				sl.add(body)
			},
		),
	)

	// wait for consumer service to be started
	cnf.HttpClient.SetServiceUrlPattern(server.URL + "/SERVICE")
	go app.Run(ctx, cnf)
	<-app.chConsumerStart // virtual-service
	<-app.chConsumerStart // splitting-service
	<-app.chConsumerStart // worker-1
	<-app.chConsumerStart // worker-2

	return app, queueCon, server, sl
}

func waitForServiceLog(sl *serviceLog, expectingItems int) {
	expireTime := time.Now().Add(3 * time.Second)

	for len(sl.items) < expectingItems {
		if expireTime.Before(time.Now()) {
			panic("time over")
		}

		time.Sleep(200 * time.Microsecond)
	}

	if len(sl.items) != expectingItems {
		panic("unexpected publishing messages")
	}
}

func TestConsumerBasic(t *testing.T) {
	forever := make(chan bool)
	app, con, server, sl := getModels(forever)
	ch := channel(con, "topic", "events")

	defer func() {
		app.Terminate()
		ch.QueuePurge("qa:virtual-service", false)
		ch.Close()
		con.Close()
		server.Close()
		logrus.WithField("test", t.Name()).Infoln("DONE")
	}()

	// Start publishing some messages.
	msgs := []struct {
		key  string
		body []byte
	}{
		{"user.create", []byte(`{"INFO": "do user create"}`)},
		{"user.create", []byte(``)},
		{"user.update", []byte(`{"id": 1, "INFO": "do user update", "original": {}}`)},
		{"user.delete", []byte(`{"INFO": "do user delete"}"`)},
	}

	for _, row := range msgs {
		ch.Publish("events", row.key, false, false, amqp.Publishing{Body: row.body})
	}

	waitForServiceLog(sl, 3)
	if !bytes.Contains(sl.items[0], []byte("user.create")) ||
		!bytes.Contains(sl.items[1], []byte("user.create")) {
		t.Error("message `user.create` is not delivered to service.")
		t.Fail()
	}

	if !bytes.Contains(sl.items[2], []byte("user.update")) {
		t.Error("message `user.update` is not delivered to service.")
		t.Fail()
	}
}

func TestMessageFilter(t *testing.T) {
	forever := make(chan bool)
	app, con, server, sl := getModels(forever)
	ch := channel(con, "topic", "events")

	defer func() {
		app.Terminate()
		ch.QueuePurge("qa:virtual-service", false)
		ch.Close()
		con.Close()
		server.Close()
		logrus.WithField("test", t.Name()).Infoln("DONE")
	}()

	// Start publishing some messages.
	msgs := []struct {
		key  string
		body []byte
	}{
		{"lo.update", []byte(`{"type":"course", "id": 666, "title": "Course X", "original": {}`)},
		{"lo.update", []byte(`{"type":"event",  "id": 555, "title": "My event", "original": {}}`)},
	}

	for _, row := range msgs {
		ch.Publish("events", row.key, false, false, amqp.Publishing{Body: row.body})
	}

	waitForServiceLog(sl, 1)
	if !bytes.Contains(sl.items[0], []byte("My event")) {
		t.Error("message `user.update` is not delivered to service.")
	}
}

func TestMessageSplit(t *testing.T) {
	forever := make(chan bool)
	app, con, server, sl := getModels(forever)
	ch := channel(con, "topic", "events")

	time.Sleep(2 * time.Second)

	defer func() {
		app.Terminate()
		ch.QueuePurge("qa:virtual-service", false)
		ch.Close()
		con.Close()
		server.Close()
		logrus.WithField("test", t.Name()).Infoln("DONE")
	}()

	// By portal-name & entity-type
	// ---------------------
	msg := amqp.Publishing{
		Body:    []byte(`{"INFO": "DELETE R.O"}`),
		Headers: amqp.Table{"portal-name": "qa.mygo1.com", "entity-id": "ro"},
	}

	ch.Publish("events", "ro.delete", false, false, msg)
	waitForServiceLog(sl, 1)
	if !bytes.Contains(sl.items[0], []byte(`"X-QUEUE":"qa:splitting-service"`)) {
		t.Error("message is not split")
		t.Fail()
	}

	// By portal-id & entity-type
	// ---------------------
	for portalId := 100; portalId < 127; portalId++ { // 101 => 127
		sl.reset()
		msg.Headers = amqp.Table{"portal-name": strconv.Itoa(portalId), "entity-type": "ro"}
		ch.Publish("events", "ro.delete", false, false, msg)

		waitForServiceLog(sl, 1)
		if !bytes.Contains(sl.items[0], []byte(`"X-ORIGINAL-ROUTING-KEY":"qa:splitting-service:0"`)) {
			t.Error("message is not split")
			t.Fail()
		}
	}
}

func TestLazyQueue(t *testing.T) {
	forever := make(chan bool)
	app, con, server, sl := getModels(forever)
	ch := channel(con, "topic", "events")

	defer func() {
		app.Terminate()
		ch.QueuePurge("qa:virtual-service", false)
		ch.Close()
		con.Close()
		server.Close()
		logrus.WithField("test", t.Name()).Infoln("DONE")
	}()

	// Start publishing some messages.
	msgs := []struct {
		key  string
		body []byte
	}{
		{"do.mail.send", []byte(`{"INFO":"checkLazyQueue"}`)},
		{"do.mail.flush", []byte(`{"INFO":"checkLazyQueue"}`)},
	}

	for _, row := range msgs {
		ch.Publish("events", row.key, false, false, amqp.Publishing{Body: row.body})
	}

	waitForServiceLog(sl, 2)
	if !bytes.Contains(sl.items[0], []byte("do.mail.send")) {
		t.Error("message `do.mail.send` is not delivered to service.")
	}

	if !bytes.Contains(sl.items[1], []byte("do.mail.flush")) {
		t.Error("message `do.mail.flush` is not delivered to service.")
	}
}