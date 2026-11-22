package auth

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var signingKeyID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

type SigningKeys struct {
	active  string
	private ed25519.PrivateKey
	public  map[string]ed25519.PublicKey
}

func NewSigningKeys(seed []byte, active string, overlap map[string]string) (*SigningKeys, error) {
	if len(seed) != ed25519.SeedSize || !signingKeyID.MatchString(active) || len(overlap) > 8 {
		return nil, errors.New("invalid signing key configuration")
	}
	private := ed25519.NewKeyFromSeed(seed)
	public := map[string]ed25519.PublicKey{active: private.Public().(ed25519.PublicKey)}
	for kid, encoded := range overlap {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || !signingKeyID.MatchString(kid) || len(key) != ed25519.PublicKeySize {
			return nil, errors.New("invalid overlap public key")
		}
		if kid == active && !bytes.Equal(key, public[active]) {
			return nil, errors.New("active public key mismatch")
		}
		public[kid] = ed25519.PublicKey(key)
	}
	if len(public) > 8 {
		return nil, errors.New("too many signing keys")
	}

	return &SigningKeys{active: active, private: private, public: public}, nil
}

func NewSigningManager(keys *SigningKeys, issuer, audience string, lifetime time.Duration, legacySecret string, legacyUntil time.Time) (*Manager, error) {
	if keys == nil || strings.TrimSpace(issuer) == "" || strings.TrimSpace(audience) == "" || len(issuer) > 256 || len(audience) > 256 ||
		lifetime < time.Minute || lifetime > 15*time.Minute || lifetime%time.Second != 0 ||
		(legacySecret == "") != legacyUntil.IsZero() || legacyUntil.After(time.Now().Add(24*time.Hour)) {
		return nil, errors.New("invalid asymmetric JWT configuration")
	}
	m := NewManager(legacySecret, issuer, lifetime)
	m.keys, m.audience, m.legacyUntil = keys, audience, legacyUntil

	return m, nil
}

func (m *Manager) sign(claims Claims) (string, error) {
	if m.keys == nil {
		return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	}
	claims.Audience = jwt.ClaimStrings{m.audience}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = m.keys.active

	return token.SignedString(m.keys.private)
}

func (m *Manager) verificationKey(token *jwt.Token) (any, error) {
	if token.Method == jwt.SigningMethodHS256 && (m.keys == nil || (len(m.secret) > 0 && m.now().Before(m.legacyUntil))) {
		return m.secret, nil
	}
	if token.Method != jwt.SigningMethodEdDSA || m.keys == nil {
		return nil, errors.New("unexpected signing method")
	}
	kid, ok := token.Header["kid"].(string)
	key, found := m.keys.public[kid]
	if !ok || !found {
		return nil, errors.New("unknown signing key")
	}

	return key, nil
}

type publicSigningKey struct {
	KTY string `json:"kty"`
	CRV string `json:"crv"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	KID string `json:"kid"`
	X   string `json:"x"`
}

func (m *Manager) JWKS() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := struct {
			Keys []publicSigningKey `json:"keys"`
		}{Keys: []publicSigningKey{}}
		if m.keys != nil {
			ids := make([]string, 0, len(m.keys.public))
			for id := range m.keys.public {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			for _, id := range ids {
				result.Keys = append(result.Keys, publicSigningKey{"OKP", "Ed25519", "sig", "EdDSA", id, base64.RawURLEncoding.EncodeToString(m.keys.public[id])})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_ = json.NewEncoder(w).Encode(result)
	})
}
