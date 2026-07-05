package api

import (
	"sync"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
)

type openAIRateLimiter struct {
	mu      sync.Mutex
	clock   func() time.Time
	windows map[string][]openAIRateLimitEvent
}

type openAIRateLimitEvent struct {
	at       time.Time
	requests int
	tokens   int
}

func newOpenAIRateLimiter() *openAIRateLimiter {
	return &openAIRateLimiter{
		clock:   time.Now,
		windows: make(map[string][]openAIRateLimitEvent),
	}
}

func (api *WorkflowAPI) checkOpenAIRequestRateLimit(key *storage.APIKey) time.Duration {
	if key == nil || api.rateLimiter == nil {
		return 0
	}
	return api.rateLimiter.reserve(key.ID, key.RequestsPerMinute, 0, 1, 0)
}

func (api *WorkflowAPI) checkOpenAITokenRateLimit(key *storage.APIKey, tokens int) time.Duration {
	if key == nil || api.rateLimiter == nil || tokens <= 0 {
		return 0
	}
	return api.rateLimiter.reserve(key.ID, 0, key.TokensPerMinute, 0, tokens)
}

func (l *openAIRateLimiter) reserve(keyID string, requestLimit, tokenLimit, requests, tokens int) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock().UTC()
	cutoff := now.Add(-time.Minute)
	l.pruneExpiredLocked(cutoff)
	events := l.windows[keyID]

	var tokenSum int
	var requestSum int
	for _, event := range events {
		requestSum += event.requests
		tokenSum += event.tokens
	}
	if requestLimit > 0 && requestSum+requests > requestLimit {
		l.windows[keyID] = events
		return retryAfterFor(events[0].at, now)
	}
	if tokenLimit > 0 && tokens > 0 && tokenSum+tokens > tokenLimit {
		l.windows[keyID] = events
		if len(events) == 0 {
			return time.Minute
		}
		return retryAfterFor(events[0].at, now)
	}
	events = append(events, openAIRateLimitEvent{at: now, requests: requests, tokens: tokens})
	l.windows[keyID] = events
	return 0
}

func (l *openAIRateLimiter) pruneExpiredLocked(cutoff time.Time) {
	for keyID, events := range l.windows {
		filtered := events[:0]
		for _, event := range events {
			if event.at.After(cutoff) {
				filtered = append(filtered, event)
			}
		}
		if len(filtered) == 0 {
			delete(l.windows, keyID)
			continue
		}
		l.windows[keyID] = filtered
	}
}

func retryAfterFor(first, now time.Time) time.Duration {
	wait := first.Add(time.Minute).Sub(now)
	if wait < time.Second {
		return time.Second
	}
	return wait
}
