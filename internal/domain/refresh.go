package domain

import "time"

// SessionTokens передаёт секреты только вызывающему внутреннему auth-транспорту.
type SessionTokens struct {
	User             *User
	UserID           string
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// RefreshGrant содержит проверенный снимок прав для подписи новой access-сессии.
type RefreshGrant struct {
	User             *User
	UserID           string
	Roles            []string
	SecurityVersion  int64
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}
