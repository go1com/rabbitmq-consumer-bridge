package rabbitmq_consumer_bridge

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

type PrometheusConfig struct {
	Server string `yaml:"server"`
}

var promSuccessMessageCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "consumer_total_success_message",
		Help: "The number of message successful processed",
	},
	[]string{"queue", "service", "routing_key"},
)

var promFailureMessageCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "consumer_total_failure_message",
		Help: "The number of message unsuccessful processed",
	},
	[]string{"queue", "service", "routing_key"},
)

var promRetryMessageCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "consumer_total_retry_message",
		Help: "The number of message was tried",
	},
	[]string{"queue", "service", "routing_key"},
)

var promFilteredMessageCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "consumer_total_filtered_message",
		Help: "Total messages filtered",
	},
	[]string{"queue", "service", "routing_key"},
)

var promDurationHistogram = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "consumer_consume_duration_seconds",
		Help:    "Consume duration distribution",
		Buckets: []float64{1, 2, 5, 10, 20, 60},
	},
	[]string{"queue", "service", "routing_key"},
)

func startPrometheusServer(ctx context.Context, addr string) {
	prometheus.MustRegister(promDurationHistogram)
	prometheus.MustRegister(promSuccessMessageCounter)
	prometheus.MustRegister(promFailureMessageCounter)
	prometheus.MustRegister(promRetryMessageCounter)
	prometheus.MustRegister(promFilteredMessageCounter)

	logrus.
		WithField("add", addr).
		Infoln("starting admin HTTP server")

	http.Handle("/metrics", promhttp.Handler())
	panic(http.ListenAndServe(addr, logRequest(http.DefaultServeMux)))
}

func logRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logrus.
			WithField("RemoteAddr", r.RemoteAddr).
			WithField("Method", r.Method).
			WithField("URL", r.URL).
			Info("History")
		handler.ServeHTTP(w, r)
	})
}
