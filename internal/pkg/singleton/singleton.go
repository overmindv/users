package singleton

import "sync"

// Provider initializes a process-scoped dependency exactly once. Containers own
// providers, so tests can create isolated containers without global state.
type Provider[T any] struct {
	once  sync.Once
	value T
	err   error
}

func (p *Provider[T]) Get(factory func() (T, error)) (T, error) {
	p.once.Do(func() { p.value, p.err = factory() })

	return p.value, p.err
}
