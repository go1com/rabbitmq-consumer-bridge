package rabbitmq_consumer_bridge

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/sirupsen/logrus"
)

type Application struct {
	version          string
	env              string
	stop             chan bool
	groupProcess     sync.WaitGroup
	ServiceTerminate []chan bool
	config           *AppConfig
	chConsumerStart  chan bool
}

const (
	Version     = "v5.0"
	Environment = "dev"
)

var (
	mu          sync.Mutex
	initialized uint32
	app         *Application
)

func NewApp(forever chan bool) *Application {
	if 1 == atomic.LoadUint32(&initialized) {
		return app
	}

	mu.Lock()
	defer mu.Unlock()

	if 0 == initialized {
		app = &Application{
			version:          Version,
			env:              Env("ENVIRONMENT", Environment),
			stop:             forever,
			groupProcess:     sync.WaitGroup{},
			ServiceTerminate: []chan bool{},
			chConsumerStart:  make(chan bool, 111),
		}

		atomic.StoreUint32(&initialized, 1)
	}

	return app
}

func (app *Application) Run(ctx context.Context, cnf *AppConfig) {
	app.config = cnf
	app.startServices(ctx, cnf)

	if nil != cnf.Prometheus {
		go startPrometheusServer(ctx, cnf.Prometheus.Server)
	}
}

func (app *Application) startServices(ctx context.Context, cnf *AppConfig) {
	for i := range cnf.Services {
		for w := 0; w < cnf.Services[i].Worker; w++ {
			mu.Lock()
			service, err := NewService(cnf, &cnf.Services[i], w)

			if nil != err {
				logrus.
					WithError(err).
					Panic("failed starting service")
			}

			terminate := make(chan bool, cnf.Services[i].Split+1)
			app.ServiceTerminate = append(app.ServiceTerminate, terminate)
			go service.start(ctx, terminate)

			mu.Unlock()
		}
	}
}

func (app *Application) Terminate() {
	for index, terminate := range app.ServiceTerminate {
		terminate <- true

		if app.config.Services[index].Split > 0 {
			for i := 0; i < app.config.Services[index].Split; i++ {
				terminate <- true
			}
		}
	}

	app.groupProcess.Wait()
}
