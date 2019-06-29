package main

import (
	"context"
	"os"

	"github.com/sirupsen/logrus"

	"go1/consumer"
)

func main() {
	ctx := context.Background()
	forever := make(chan bool)
	app := consumer.NewApp(forever)
	cnf := consumer.AppConfigFromEnv()

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
