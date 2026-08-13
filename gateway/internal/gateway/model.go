package gateway

import (
	"strings"
	"time"

	"permit-gateway/internal/urlpolicy"
)

type Resource struct {
	ID                  string
	PublicID            string
	DisplayName         string
	Origin              urlpolicy.Normalized
	Public              bool
	AllowedPathPrefixes []string
	AllowedMethods      map[string]bool
	WebSocketEnabled    bool
}

func (r Resource) AllowsPath(candidate string) bool {
	for _, prefix := range r.AllowedPathPrefixes {
		if prefix == "/" || candidate == prefix || strings.HasPrefix(candidate, prefix+"/") {
			return true
		}
	}
	return false
}

type launchState struct {
	ResourceID string
	Target     urlpolicy.Normalized
	ActorID    string
	ExpiresAt  time.Time
}

type sessionState struct {
	ResourceID string
	ActorID    string
	ExpiresAt  time.Time
}
