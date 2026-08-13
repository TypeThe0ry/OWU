package gateway

import (
	"sync"
	"time"
)

type rateWindow struct {
	start time.Time
	count int
}

type limiter struct {
	mu      sync.Mutex
	limit   int
	period  time.Duration
	windows map[string]rateWindow
}

func newLimiter(limit int, period time.Duration) *limiter {
	return &limiter{limit: limit, period: period, windows: map[string]rateWindow{}}
}

func (l *limiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	window := l.windows[key]
	if window.start.IsZero() || now.Sub(window.start) >= l.period {
		window = rateWindow{start: now}
	}
	if window.count >= l.limit {
		return false, l.period - now.Sub(window.start)
	}
	window.count++
	l.windows[key] = window
	return true, 0
}
