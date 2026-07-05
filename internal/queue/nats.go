// Package queue encapsula a conexão NATS JetStream.
package queue

import (
	"time"

	"github.com/nats-io/nats.go"
)

// NewNATS conecta com retry/backoff exponencial.
func NewNATS(url string) (*nats.Conn, error) {
	return nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.Timeout(5*time.Second),
		nats.Name("PixelAudit"),
	)
}
