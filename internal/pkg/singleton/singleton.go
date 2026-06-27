package singleton

import "context"

type Singleton struct{}

type Repositories struct{}

// instance - объект для получения конфигураций
var instance *Singleton

// GetSingleton - возвращает экземпляр Singleton
func GetSingleton(c ...context.Context) *Singleton {
	return instance
}
