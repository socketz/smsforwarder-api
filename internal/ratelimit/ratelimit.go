// Package ratelimit is a tiny per-user fixed-window limiter (in-memory).
// Enough to stop a single account from flooding /api/sms.
package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu       sync.Mutex
	perMin   int
	counters map[int64]*window
}

type window struct {
	start time.Time
	count int
}

func New(perMin int) *Limiter {
	if perMin <= 0 {
		perMin = 60
	}
	return &Limiter{perMin: perMin, counters: make(map[int64]*window)}
}

// Allow reports whether userID may perform one more action in the current minute.
func (l *Limiter) Allow(userID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	w := l.counters[userID]
	if w == nil || now.Sub(w.start) >= time.Minute {
		l.counters[userID] = &window{start: now, count: 1}
		return true
	}
	if w.count >= l.perMin {
		return false
	}
	w.count++
	return true
}
