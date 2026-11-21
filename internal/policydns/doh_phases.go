package policydns

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http/httptrace"
	"sync"
	"time"
)

var errDoHPhaseTimeout = errors.New("DoH connection setup or reused response stalled")

func monitorDoHPhases(ctx context.Context, limit time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	var mu sync.Mutex
	var timer *time.Timer
	var connected, reused, received, finished bool

	stopTimer := func() {
		if timer != nil {
			timer.Stop()
			timer = nil
		}
	}
	startTimer := func() {
		stopTimer()
		if finished {
			return
		}
		var current *time.Timer
		current = time.AfterFunc(limit, func() {
			mu.Lock()
			defer mu.Unlock()
			if timer == current {
				cancel(errDoHPhaseTimeout)
			}
		})
		timer = current
	}
	trace := &httptrace.ClientTrace{
		GetConn: func(string) {
			mu.Lock()
			defer mu.Unlock()
			stopTimer()
			connected, reused, received = false, false, false
		},
		TLSHandshakeStart: func() {
			mu.Lock()
			defer mu.Unlock()
			if !connected {
				startTimer()
			}
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			mu.Lock()
			defer mu.Unlock()
			if !connected {
				stopTimer()
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			defer mu.Unlock()
			stopTimer()
			connected, reused = true, info.Reused
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			mu.Lock()
			defer mu.Unlock()
			if reused && !received && info.Err == nil {
				startTimer()
			}
		},
		GotFirstResponseByte: func() {
			mu.Lock()
			defer mu.Unlock()
			received = true
			stopTimer()
		},
	}
	return httptrace.WithClientTrace(ctx, trace), func() {
		mu.Lock()
		finished = true
		stopTimer()
		mu.Unlock()
		cancel(nil)
	}
}
