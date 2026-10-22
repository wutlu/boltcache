// Package auth manages API tokens shared by every protocol: REST requests
// (Authorization: Bearer / X-API-Token / ?token=), and RESP/TCP clients
// (AUTH <token>).
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// TokenInfo describes a token without exposing it.
type TokenInfo struct {
	ID         string    `json:"id"` // masked token, safe to display
	CreatedAt  time.Time `json:"created_at"`
	LastUsed   time.Time `json:"last_used,omitzero"`
	UsageCount int64     `json:"usage_count"`
	FromConfig bool      `json:"from_config"`
}

type token struct {
	value string
	info  TokenInfo
}

// Manager stores tokens. It is safe for concurrent use.
type Manager struct {
	enabled bool

	mu     sync.RWMutex
	tokens map[string]*token
}

// NewManager creates a manager. When enabled is false every check succeeds.
func NewManager(enabled bool, tokens []string) *Manager {
	m := &Manager{enabled: enabled, tokens: map[string]*token{}}
	now := time.Now()
	for _, t := range tokens {
		m.tokens[t] = &token{value: t, info: TokenInfo{ID: Mask(t), CreatedAt: now, FromConfig: true}}
	}
	return m
}

// Enabled reports whether authentication is required.
func (m *Manager) Enabled() bool { return m.enabled }

// Check validates a token using constant-time comparison and records usage.
func (m *Manager) Check(candidate string) bool {
	if !m.enabled {
		return true
	}
	if candidate == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var found *token
	for _, t := range m.tokens {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(t.value)) == 1 {
			found = t
		}
	}
	if found == nil {
		return false
	}
	found.info.LastUsed = time.Now()
	found.info.UsageCount++
	return true
}

// Generate creates, stores and returns a new random token. Generated tokens
// live in memory only and disappear on restart.
func (m *Manager) Generate() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	t := "bc_" + hex.EncodeToString(b)
	m.mu.Lock()
	m.tokens[t] = &token{value: t, info: TokenInfo{ID: Mask(t), CreatedAt: time.Now()}}
	m.mu.Unlock()
	return t
}

// Revoke removes a token identified by its value or its masked ID. Tokens
// from the config file can't be revoked at runtime.
func (m *Manager) Revoke(idOrToken string) (removed bool, fromConfig bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for v, t := range m.tokens {
		if v == idOrToken || t.info.ID == idOrToken {
			if t.info.FromConfig {
				return false, true
			}
			delete(m.tokens, v)
			return true, false
		}
	}
	return false, false
}

// List returns token metadata sorted by creation time.
func (m *Manager) List() []TokenInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TokenInfo, 0, len(m.tokens))
	for _, t := range m.tokens {
		out = append(out, t.info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Mask hides most of a token: "dev-token-123" -> "dev-…123".
func Mask(t string) string {
	if len(t) <= 8 {
		return strings.Repeat("*", len(t))
	}
	return t[:4] + "…" + t[len(t)-4:]
}

// FromRequest extracts a token from an HTTP request: Authorization header
// ("Bearer x", "Token x" or the bare token), X-API-Token header or the
// token query parameter (needed for browser WebSockets).
func FromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		for _, p := range []string{"Bearer ", "Token "} {
			if len(h) > len(p) && strings.EqualFold(h[:len(p)], p) {
				return strings.TrimSpace(h[len(p):])
			}
		}
		return strings.TrimSpace(h)
	}
	if t := r.Header.Get("X-API-Token"); t != "" {
		return t
	}
	return r.URL.Query().Get("token")
}
