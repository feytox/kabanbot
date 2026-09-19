package chat

import (
	"sync"
	"time"

	"github.com/feytox/kabanbot/internal/domain"
)

// limitWindow is the period the per-hour limits count over.
const limitWindow = time.Hour

// RateLimitError means the chat or the user asked the bot too often.
type RateLimitError struct {
	// PerUser is true if the user's own limit was hit, false for the chat's.
	PerUser bool
	// RetryIn is when the next answer becomes possible.
	RetryIn time.Duration
}

func (e *RateLimitError) Error() string {
	return "rate limited, retry in " + e.RetryIn.Round(time.Second).String()
}

type limitKey struct{ chatID, userID int64 }

// Limiter counts answers per chat and per user in a sliding hour. It lives in memory,
// so a restart forgets the counts.
type Limiter struct {
	mu   sync.Mutex
	hits map[limitKey][]time.Time
}

// NewLimiter creates a Limiter.
func NewLimiter() *Limiter { return &Limiter{hits: make(map[limitKey][]time.Time)} }

// Allow records an answer to the user in the chat if both limits allow it.
func (l *Limiter) Allow(chatID, userID int64, lim domain.RateLimits) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	user, chat := limitKey{chatID, userID}, limitKey{chatID: chatID}
	if wait, ok := l.check(user, lim.UserPerHour, now); !ok {
		return &RateLimitError{PerUser: true, RetryIn: wait}
	}
	if wait, ok := l.check(chat, lim.ChatPerHour, now); !ok {
		return &RateLimitError{RetryIn: wait}
	}
	l.hits[user] = append(l.hits[user], now)
	l.hits[chat] = append(l.hits[chat], now)
	return nil
}

// check drops hits older than the window and reports whether one more fits.
// If not, it returns how long until the oldest hit leaves the window.
func (l *Limiter) check(k limitKey, limit int, now time.Time) (time.Duration, bool) {
	hits := l.hits[k]
	i := 0
	for i < len(hits) && now.Sub(hits[i]) >= limitWindow {
		i++
	}
	hits = hits[i:]
	if len(hits) == 0 {
		delete(l.hits, k)
	} else {
		l.hits[k] = hits
	}
	if limit <= 0 || len(hits) < limit {
		return 0, true
	}
	return limitWindow - now.Sub(hits[0]), false
}
