package api

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"os"
	"strings"
)

type principal struct {
	id     string
	scopes map[string]struct{}
}

func (p principal) hasScope(scope string) bool {
	_, ok := p.scopes[scope]
	return ok
}

func (p principal) canAccessTenant(tenant string) bool {
	if tenant == "" {
		tenant = "default"
	}
	if p.hasScope("operator") {
		return true
	}
	_, ok := p.scopes["tenant:"+tenant]
	return ok
}

type authorizer struct {
	enabled bool
	tokens  map[string]principal
}

func newAuthorizerFromEnv() *authorizer {
	raw := strings.TrimSpace(os.Getenv("DAEF_API_TOKENS"))
	if raw == "" {
		return &authorizer{enabled: false, tokens: map[string]principal{}}
	}
	tokens := make(map[string]principal)
	entries := strings.Split(raw, ",")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		token := strings.TrimSpace(parts[0])
		scopeRaw := strings.TrimSpace(parts[1])
		if token == "" || scopeRaw == "" {
			continue
		}
		scopes := make(map[string]struct{})
		for _, s := range strings.Split(scopeRaw, "|") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			scopes[s] = struct{}{}
		}
		if len(scopes) == 0 {
			continue
		}
		tokens[token] = principal{id: tokenID(token), scopes: scopes}
	}
	if len(tokens) == 0 {
		return &authorizer{enabled: false, tokens: map[string]principal{}}
	}
	return &authorizer{enabled: true, tokens: tokens}
}

func (a *authorizer) authorize(r *http.Request, requiredAny ...string) (principal, int, string) {
	if !a.enabled {
		return principal{id: "anonymous", scopes: map[string]struct{}{}}, http.StatusOK, ""
	}
	token := bearerToken(r)
	if token == "" {
		return principal{}, http.StatusUnauthorized, "missing bearer token"
	}
	p, ok := a.tokens[token]
	if !ok {
		return principal{}, http.StatusUnauthorized, "invalid token"
	}
	if len(requiredAny) == 0 {
		return p, http.StatusOK, ""
	}
	for _, scope := range requiredAny {
		if _, ok := p.scopes[scope]; ok {
			return p, http.StatusOK, ""
		}
	}
	return p, http.StatusForbidden, fmt.Sprintf("missing required scope (one of: %s)", strings.Join(requiredAny, ","))
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[len("Bearer "):])
	}
	return strings.TrimSpace(r.Header.Get("X-DAEF-Token"))
}

func tokenID(token string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(token))
	return fmt.Sprintf("tok-%08x", h.Sum32())
}
