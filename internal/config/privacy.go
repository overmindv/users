package config

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/overmindv/users/internal/domain"
	"github.com/overmindv/users/internal/privacyowner"
)

type Privacy struct {
	Enabled bool
	Owners  map[string]privacyowner.Endpoint
}

func loadPrivacy() (Privacy, error) {
	enabled := os.Getenv("USERS_PRIVACY_ENABLED")
	raw := os.Getenv("USERS_PRIVACY_OWNERS")
	if enabled == "" || enabled == "false" {
		if raw != "" {
			return Privacy{}, errors.New("privacy owners require enabled privacy")
		}

		return Privacy{}, nil
	}
	if enabled != "true" {
		return Privacy{}, errors.New("invalid privacy enabled flag")
	}
	var owners map[string]privacyowner.Endpoint
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&owners) != nil || decoder.Decode(new(any)) != io.EOF || len(owners) != len(domain.PrivacyOwners) {
		return Privacy{}, errors.New("all privacy owners are required")
	}
	tokens := map[string]bool{}
	for _, owner := range domain.PrivacyOwners {
		endpoint, ok := owners[owner]
		parsed, err := url.Parse(endpoint.URL)
		if !ok || err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(endpoint.Token) < 32 || len(endpoint.Token) > 4096 || tokens[endpoint.Token] {
			return Privacy{}, errors.New("invalid privacy owner configuration")
		}
		ip := net.ParseIP(parsed.Hostname())
		local := parsed.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
		if parsed.Scheme != "https" && (parsed.Scheme != "http" || !local) {
			return Privacy{}, errors.New("privacy owners require HTTPS")
		}
		tokens[endpoint.Token] = true
	}

	return Privacy{Enabled: true, Owners: owners}, nil
}
