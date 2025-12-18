package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type AuthManager struct {
	config *Config
	tokens map[string]*TokenInfo
}

type TokenInfo struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used"`
	UsageCount int64    `json:"usage_count"`
}

func NewAuthManager(config *Config) *AuthManager {
	auth := &AuthManager{
		config: config,
		tokens: make(map[string]*TokenInfo),
	}
	
	// Load configured tokens
	for _, token := range config.Security.Auth.Tokens {
		auth.tokens[token] = &TokenInfo{
			Token:     token,
			CreatedAt: time.Now(),
			LastUsed:  time.Now(),
		}
	}
	
	return auth
}

func (a *AuthManager) ValidateToken(token string) bool {
	if !a.config.Security.Auth.Enabled {
		return true // Auth disabled
	}
	
	if token == "" {
		return false
	}
	
	tokenInfo, exists := a.tokens[token]
	if !exists {
		return false
	}
	
	// Update usage stats
	tokenInfo.LastUsed = time.Now()
	tokenInfo.UsageCount++
	
	return true
}

func (a *AuthManager) GenerateToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (a *AuthManager) AddToken(token string) {
	a.tokens[token] = &TokenInfo{
		Token:     token,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}
}

func (a *AuthManager) RemoveToken(token string) {
	delete(a.tokens, token)
}

func (a *AuthManager) ListTokens() map[string]*TokenInfo {
	return a.tokens
}

// HTTP Middleware
func (a *AuthManager) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.config.Security.Auth.Enabled {
			next.ServeHTTP(w, r)
			return
		}
		
		// Skip auth for health/info endpoints
		if r.URL.Path == "/health" || r.URL.Path == "/ping" {
			next.ServeHTTP(w, r)
			return
		}
		
		token := a.extractToken(r)
		if !a.ValidateToken(token) {
			a.sendAuthError(w)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

func (a *AuthManager) extractToken(r *http.Request) string {
	// Try Authorization header first
	auth := r.Header.Get("Authorization")
	if auth != "" {
		// Support "Bearer token" format
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
		// Support "Token token" format
		if strings.HasPrefix(auth, "Token ") {
			return strings.TrimPrefix(auth, "Token ")
		}
		// Direct token
		return auth
	}
	
	// Try X-API-Token header
	if token := r.Header.Get("X-API-Token"); token != "" {
		return token
	}
	
	// Try query parameter
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}
	
	return ""
}

func (a *AuthManager) sendAuthError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `{"success": false, "error": "Authentication required"}`)
}