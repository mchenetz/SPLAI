package telemetry

import "github.com/example/splai/internal/observability"

type Client interface {
	Incr(name string)
}

type nop struct{}
type metrics struct{}

func NewNop() Client {
	return nop{}
}

func New() Client {
	return metrics{}
}

func (nop) Incr(name string) {
	_ = name
}

func (metrics) Incr(name string) {
	observability.Default.IncCounter(name, map[string]string{"component": "worker"}, 1)
}
