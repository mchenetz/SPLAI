package telemetry

type Client interface {
	Incr(name string)
}

type nop struct{}

func NewNop() Client {
	return nop{}
}

func (nop) Incr(name string) {
	_ = name
}
