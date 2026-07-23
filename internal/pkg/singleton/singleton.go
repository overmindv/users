package singleton

import "sync"

// Provider инициализирует process-scoped зависимость ровно один раз.
// Container владеет provider, поэтому тесты могут создавать изолированные container без global mutable state.
type Provider[T any] struct {
	once  sync.Once
	value T
	err   error
}

// Get возвращает сохранённое значение или создаёт его через factory при первом вызове.
func (p *Provider[T]) Get(factory func() (T, error)) (T, error) {
	p.once.Do(func() { p.value, p.err = factory() })

	return p.value, p.err
}
