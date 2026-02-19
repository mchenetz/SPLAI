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
	if p.hasScope("tenant:*") {
		return true
	}
	_, ok := p.scopes["tenant:"+tenant]
	return ok
}

func (p principal) canTenantAction(tenant, action string) bool {
	if p.hasScope("operator") || p.hasScope("admin") {
		return true
	}
	if tenant == "" {
		tenant = "default"
	}
	if p.hasScope("tenant:*") {
		return true
	}
	if p.hasScope("tenant:" + tenant) {
		if p.hasScope("role:tenant-reader") {
			return action == "read"
		}
		if p.hasScope("role:tenant-runner") {
			return action == "read" || action == "submit" || action == "report"
		}
		if !p.hasScope("job:read") && !p.hasScope("job:submit") && !p.hasScope("task:report") && !p.hasScope("job:cancel") {
			return action == "read" || action == "submit" || action == "report" || action == "cancel"
		}
		switch action {
		case "read":
			return true
		case "submit", "report":
			return p.hasScope("job:submit") || p.hasScope("task:report") || p.hasScope("job:read") || p.hasScope("job:cancel")
		case "cancel":
			return p.hasScope("job:cancel") || p.hasScope("job:submit")
		default:
			return true
		}
	}
	switch action {
	case "read":
		return p.hasScope("job:read")
	case "submit":
		return p.hasScope("job:submit")
	case "report":
		return p.hasScope("task:report")
	case "cancel":
		return p.hasScope("job:cancel")
	default:
		return false
	}
}

type authorizer struct {
	enabled bool
	tokens  map[string]principal
}

func newAuthorizerFromEnv() *authorizer {
	roleScopes := defaultRoleScopes()
	for role, scopes := range parseRoleScopes(strings.TrimSpace(os.Getenv("SPLAI_API_ROLES"))) {
		roleScopes[role] = scopes
	}
	tokenRoles := parseTokenRoles(strings.TrimSpace(os.Getenv("SPLAI_API_TOKEN_ROLES")))
	raw := strings.TrimSpace(os.Getenv("SPLAI_API_TOKENS"))
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
		for _, role := range tokenRoles[token] {
			scopes["role:"+role] = struct{}{}
			for scope := range roleScopes[role] {
				scopes[scope] = struct{}{}
			}
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
	return strings.TrimSpace(r.Header.Get("X-SPLAI-Token"))
}

func tokenID(token string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(token))
	return fmt.Sprintf("tok-%08x", h.Sum32())
}

func parseRoleScopes(raw string) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	if raw == "" {
		return out
	}
	entries := strings.Split(raw, ",")
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		role := strings.TrimSpace(parts[0])
		scopeRaw := strings.TrimSpace(parts[1])
		if role == "" || scopeRaw == "" {
			continue
		}
		scopes := map[string]struct{}{}
		for _, s := range strings.Split(scopeRaw, "|") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			scopes[s] = struct{}{}
		}
		if len(scopes) > 0 {
			out[role] = scopes
		}
	}
	return out
}

func parseTokenRoles(raw string) map[string][]string {
	out := map[string][]string{}
	if raw == "" {
		return out
	}
	entries := strings.Split(raw, ",")
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		token := strings.TrimSpace(parts[0])
		roleRaw := strings.TrimSpace(parts[1])
		if token == "" || roleRaw == "" {
			continue
		}
		roles := make([]string, 0, 4)
		for _, r := range strings.Split(roleRaw, "|") {
			r = strings.TrimSpace(r)
			if r != "" {
				roles = append(roles, r)
			}
		}
		if len(roles) > 0 {
			out[token] = roles
		}
	}
	return out
}

func defaultRoleScopes() map[string]map[string]struct{} {
	mk := func(vals ...string) map[string]struct{} {
		out := map[string]struct{}{}
		for _, v := range vals {
			out[v] = struct{}{}
		}
		return out
	}
	return map[string]map[string]struct{}{
		"admin":         mk("operator", "metrics", "admin", "tenant:*", "job:submit", "job:read", "job:cancel", "task:report"),
		"ops":           mk("operator", "metrics"),
		"tenant-runner": mk("job:submit", "job:read", "task:report"),
		"tenant-reader": mk("job:read"),
	}
}
