// Package natsx builds the NATS JetStream connection. NATS is transit, not
// truth (internal-events-nats.md): connections reconnect forever, and
// shutdown drains so in-flight messages finish before the pod exits
// (the ws-gateway drain story depends on this).
package natsx

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config for the NATS connection.
type Config struct {
	URL string
	// Name identifies the deployable in NATS monitoring (server connz).
	Name string
}

// Connect establishes the connection and the JetStream context.
// Reconnection is infinite by design: a service must ride out a NATS
// rolling restart without human help. Callers own Drain/Close.
func Connect(cfg Config) (*nats.Conn, jetstream.JetStream, error) {
	nc, err := nats.Connect(cfg.URL,
		nats.Name(cfg.Name),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.Timeout(5*time.Second),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("natsx: connect %s: %w", cfg.URL, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("natsx: jetstream context: %w", err)
	}
	return nc, js, nil
}
