package events

import (
	"context"
)

// Event represents a generic system event (e.g., "ORDER_CREATED", "PRINT_JOB")
type Event struct {
	Topic    string
	TenantID string
	Payload  []byte
}

// PubSubClient defines the interface for event streaming.
// This ensures the application is decoupled from the underlying tech.
// Currently it might use Firebase, but can seamlessly swap to Redis.
type PubSubClient interface {
	Publish(ctx context.Context, event Event) error
	Subscribe(ctx context.Context, topic string, handler func(Event)) error
	Close() error
}

// LocalPubSub is an in-memory implementation of the PubSubClient for testing and single-node execution
type LocalPubSub struct {
	handlers map[string][]func(Event)
}

func NewLocalPubSub() *LocalPubSub {
	return &LocalPubSub{
		handlers: make(map[string][]func(Event)),
	}
}

func (ps *LocalPubSub) Publish(ctx context.Context, event Event) error {
	if funcs, ok := ps.handlers[event.Topic]; ok {
		for _, f := range funcs {
			go f(event) // Execute asynchronously like a real event bus
		}
	}
	return nil
}

func (ps *LocalPubSub) Subscribe(ctx context.Context, topic string, handler func(Event)) error {
	ps.handlers[topic] = append(ps.handlers[topic], handler)
	return nil
}

func (ps *LocalPubSub) Close() error {
	return nil
}
