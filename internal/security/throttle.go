package security

import (
	"context"
	"sync"
	"time"
)

// MemoryLoginThrottler ограничивает число попыток входа по ключу (например, email)
// фиксированным окном времени. Используется только в unit-тестах; production — PostgreSQL.
type MemoryLoginThrottler struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration

	windowStart map[string]time.Time
	attempts    map[string]int
}

// NewMemoryLoginThrottler создаёт ограничитель с максимумом maxAttempts попыток за окно window.
func NewMemoryLoginThrottler(maxAttempts int, window time.Duration) *MemoryLoginThrottler {
	return &MemoryLoginThrottler{
		maxAttempts: maxAttempts,
		window:      window,
		windowStart: make(map[string]time.Time),
		attempts:    make(map[string]int),
	}
}

// Allow увеличивает счётчик попыток по ключу и возвращает true, если лимит ещё не превышен.
func (t *MemoryLoginThrottler) Allow(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	start, ok := t.windowStart[key]
	if !ok || now.Sub(start) >= t.window {
		t.windowStart[key] = now
		t.attempts[key] = 0
	}

	t.attempts[key]++

	return t.attempts[key] <= t.maxAttempts
}

// Reset обнуляет счётчик попыток после успешного входа.
func (t *MemoryLoginThrottler) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.windowStart, key)
	delete(t.attempts, key)
}

// Check реализует контракт тестового in-memory ограничителя.
func (t *MemoryLoginThrottler) Check(_ context.Context, email, _ string) (bool, error) {
	return t.Allow(email, time.Now()), nil
}

// ResetEmail реализует контракт тестового in-memory ограничителя.
func (t *MemoryLoginThrottler) ResetEmail(_ context.Context, email string) error {
	t.Reset(email)
	return nil
}
