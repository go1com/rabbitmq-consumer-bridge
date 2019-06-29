package main

import (
	"context"
	"os"

	"github.com/sirupsen/logrus"

	bridge "github.com/go1com/rabbitmq-consumer-bridge"
)

func main() {
	ctx := context.Background()
	forever := make(chan bool)
	app := bridge.NewApp(forever)
	cnf := bridge.AppConfigFromEnv()

	if cnf.Debug {
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		logrus.SetLevel(logrus.ErrorLevel)
	}

	go app.Run(ctx, cnf)

	<-forever
	app.Terminate()
	os.Exit(1)
}
