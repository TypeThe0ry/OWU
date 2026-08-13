package gateway

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"permit-gateway/internal/urlpolicy"
)

type Config struct {
	ListenAddr       string
	PublicBaseURL    string
	DemoMode         bool
	DemoTargetOrigin string
	SessionSecret    []byte
	Resources        []Resource
	TicketTTL        time.Duration
	SessionTTL       time.Duration
}

type resourceJSON struct {
	ID                  string   `json:"id"`
	PublicID            string   `json:"public_id"`
	DisplayName         string   `json:"display_name"`
	Origin              string   `json:"origin"`
	Public              bool     `json:"public"`
	AllowedPathPrefixes []string `json:"allowed_path_prefixes"`
	AllowedMethods      []string `json:"allowed_methods"`
	WebSocketEnabled    bool     `json:"websocket_enabled"`
}

func LoadConfig() (Config, error) {
	demoMode, err := strconv.ParseBool(envOr("PERMIT_DEMO_MODE", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("PERMIT_DEMO_MODE: %w", err)
	}
	config := Config{
		ListenAddr:       envOr("PERMIT_LISTEN_ADDR", ":8081"),
		PublicBaseURL:    strings.TrimRight(envOr("PERMIT_PUBLIC_BASE_URL", "http://localhost:8081"), "/"),
		DemoMode:         demoMode,
		DemoTargetOrigin: strings.TrimRight(envOr("PERMIT_DEMO_TARGET_ORIGIN", "http://demo-target:9000"), "/"),
		SessionSecret:    []byte(os.Getenv("PERMIT_SESSION_SECRET")),
		TicketTTL:        60 * time.Second,
		SessionTTL:       30 * time.Minute,
	}
	base, err := urlpolicy.Parse(config.PublicBaseURL)
	if err != nil || (base.Path != "/" || base.RawQuery != "" || base.Fragment != "") {
		return Config{}, errors.New("PERMIT_PUBLIC_BASE_URL must be a bare HTTP(S) origin")
	}
	config.PublicBaseURL = base.Origin

	if len(config.SessionSecret) < 32 {
		if !demoMode {
			return Config{}, errors.New("PERMIT_SESSION_SECRET must contain at least 32 bytes outside demo mode")
		}
		config.SessionSecret = make([]byte, 32)
		if _, err := rand.Read(config.SessionSecret); err != nil {
			return Config{}, fmt.Errorf("create demo secret: %w", err)
		}
	}

	if raw := strings.TrimSpace(os.Getenv("PERMIT_PUBLIC_RESOURCES_JSON")); raw != "" {
		var resources []resourceJSON
		if err := json.Unmarshal([]byte(raw), &resources); err != nil {
			return Config{}, fmt.Errorf("PERMIT_PUBLIC_RESOURCES_JSON: %w", err)
		}
		for _, rawResource := range resources {
			resource, err := newResource(rawResource)
			if err != nil {
				return Config{}, err
			}
			config.Resources = append(config.Resources, resource)
		}
	}
	if demoMode {
		demoResource, err := newResource(resourceJSON{
			ID: "res_demo", PublicID: "demo", DisplayName: "Permit demo service",
			Origin: config.DemoTargetOrigin, Public: true,
			AllowedPathPrefixes: []string{"/"}, WebSocketEnabled: true,
		})
		if err != nil {
			return Config{}, fmt.Errorf("demo target: %w", err)
		}
		config.Resources = append(config.Resources, demoResource)
	}
	if err := ensureUniqueResources(config.Resources); err != nil {
		return Config{}, err
	}
	return config, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func newResource(raw resourceJSON) (Resource, error) {
	if raw.ID == "" || raw.PublicID == "" || !safeIdentifier(raw.ID) || !safeIdentifier(raw.PublicID) {
		return Resource{}, errors.New("resource id and public_id must use letters, numbers, hyphens, or underscores")
	}
	normalized, err := urlpolicy.Parse(raw.Origin)
	if err != nil {
		return Resource{}, fmt.Errorf("resource %s origin: %w", raw.ID, err)
	}
	if normalized.Path != "/" || normalized.RawQuery != "" || normalized.Fragment != "" {
		return Resource{}, fmt.Errorf("resource %s origin must not include a path, query, or fragment", raw.ID)
	}
	prefixes := raw.AllowedPathPrefixes
	if len(prefixes) == 0 {
		prefixes = []string{"/"}
	}
	for i, prefix := range prefixes {
		if prefix == "" || !strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "..") {
			return Resource{}, fmt.Errorf("resource %s has an invalid path prefix", raw.ID)
		}
		prefixes[i] = strings.TrimRight(prefix, "/")
		if prefixes[i] == "" {
			prefixes[i] = "/"
		}
	}
	methods := raw.AllowedMethods
	if len(methods) == 0 {
		methods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	methodSet := make(map[string]bool, len(methods))
	for _, method := range methods {
		method = strings.ToUpper(method)
		if method == "CONNECT" || method == "TRACE" {
			return Resource{}, fmt.Errorf("resource %s cannot allow %s", raw.ID, method)
		}
		methodSet[method] = true
	}
	return Resource{
		ID: raw.ID, PublicID: raw.PublicID, DisplayName: raw.DisplayName,
		Origin: normalized, Public: raw.Public,
		AllowedPathPrefixes: prefixes, AllowedMethods: methodSet,
		WebSocketEnabled: raw.WebSocketEnabled,
	}, nil
}

func safeIdentifier(value string) bool {
	if len(value) > 80 {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func ensureUniqueResources(resources []Resource) error {
	origins, publicIDs := map[string]bool{}, map[string]bool{}
	for _, resource := range resources {
		key := resource.Origin.TupleKey()
		if origins[key] || publicIDs[resource.PublicID] {
			return fmt.Errorf("duplicate resource origin or public_id: %s", resource.ID)
		}
		origins[key], publicIDs[resource.PublicID] = true, true
	}
	return nil
}
