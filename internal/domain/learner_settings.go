package domain

import (
	"errors"
	"strings"
	"time"
	_ "time/tzdata" // IANA-база нужна и в минимальном runtime image.

	"golang.org/x/text/language"
)

var (
	ErrInvalidTimezone = errors.New("invalid timezone")
	ErrInvalidLocale   = errors.New("invalid locale")
)

const defaultLocale = "ru-RU"

// LearnerSettings принадлежит профилю Users, не состоянию обучения.
type LearnerSettings struct {
	Timezone           string
	Locale             string
	PublicName         bool
	LeaderboardVisible bool
}

func normalizeTimezone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "Local" || len(value) > 64 {
		return "", ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(value); err != nil {
		return "", ErrInvalidTimezone
	}

	return value, nil
}
func normalizeLocale(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 || strings.ContainsAny(value, "_ /\\") {
		return "", ErrInvalidLocale
	}
	tag, err := language.Parse(value)
	if err != nil {
		return "", ErrInvalidLocale
	}

	return tag.String(), nil
}
func defaultSettings(settings LearnerSettings) LearnerSettings {
	if settings.Timezone == "" {
		settings.Timezone = "UTC"
	}
	if settings.Locale == "" {
		settings.Locale = defaultLocale
	}

	return settings
}

func (u *User) Timezone() string         { return defaultSettings(u.settings).Timezone }
func (u *User) Locale() string           { return defaultSettings(u.settings).Locale }
func (u *User) PublicName() bool         { return u.settings.PublicName }
func (u *User) LeaderboardVisible() bool { return u.settings.LeaderboardVisible }

// DisplayName никогда не использует email как fallback.
func (u *User) DisplayName() string {
	if u.PublicName() {
		if name := strings.TrimSpace(u.firstName + " " + u.lastName); name != "" {
			return name
		}
	}

	return u.username.String()
}

func (u *User) patchLearnerSettings(p ProfilePatch) (LearnerSettings, error) {
	settings := defaultSettings(u.settings)
	var err error
	if p.Timezone != nil {
		settings.Timezone, err = normalizeTimezone(*p.Timezone)
		if err != nil {
			return LearnerSettings{}, err
		}
	}
	if p.Locale != nil {
		settings.Locale, err = normalizeLocale(*p.Locale)
		if err != nil {
			return LearnerSettings{}, err
		}
	}
	if p.PublicName != nil {
		settings.PublicName = *p.PublicName
	}
	if p.LeaderboardVisible != nil {
		settings.LeaderboardVisible = *p.LeaderboardVisible
	}

	return settings, nil
}
