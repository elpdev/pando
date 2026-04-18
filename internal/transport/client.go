package transport

import (
	"context"

	"github.com/elpdev/pando/internal/protocol"
)

type Event struct {
	Message *protocol.Message
	Err     error
}

type Client interface {
	Connect(context.Context) error
	Events() <-chan Event
	Send(protocol.Envelope) error
	Close() error
}
