package policydns

import (
	"context"
	"errors"
	"log"
	"net"
	"time"
)

// Temporary descriptor pressure must not permanently disable DNS/TCP. Retain
// the listening socket and existing sessions, with bounded, cancelable backoff.
func retryDNSAccept(ctx context.Context, err error, delay *time.Duration) bool {
	var temporary net.Error
	//lint:ignore SA1019 TCP Accept classification intentionally matches net/http.Server.Serve for descriptor exhaustion.
	if ctx.Err() != nil || !errors.As(err, &temporary) || !temporary.Temporary() {
		return false
	}
	if *delay == 0 {
		*delay = 5 * time.Millisecond
		log.Printf("policy-dns: temporary TCP accept failure; retrying: %v", err)
	} else {
		*delay = min(*delay*2, time.Second)
	}
	timer := time.NewTimer(*delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
