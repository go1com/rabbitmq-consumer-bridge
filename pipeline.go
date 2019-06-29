package rabbitmq_consumer_bridge

import (
	"errors"
	"fmt"
)

type PipelineConfig struct {
	Type     string                `json:"type"`
	RabbitMq *RabbitMqTargetConfig `yaml:"rabbitmq"`
}

type PipeLine interface {
	invoke([]byte) bool
}

func NewPipeLine(service *ServiceConfig) (PipeLine, error) {
	switch service.Pipeline.Type {
	case "rabbitmq":
		return NewRabbitMqPipeline(service.Pipeline.RabbitMq)

	default:
		return nil, errors.New(fmt.Sprintf("unsupported pipe: %s", service.Pipeline.Type))
	}
}
