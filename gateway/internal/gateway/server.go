package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"permit-gateway/internal/safety"
	"permit-gateway/internal/urlpolicy"
)

const (
	maxAPIRequestBytes  = 8 << 10
	maxRequestBodyBytes = 16 << 20
)

type Server struct {
	config      Config
	resources   map[string]Resource
	resourceIDs map[string]Resource
	store       *memoryStore
	safety      safety.Policy
	audit       auditor
	apiLimiter  *limiter
	dataLimiter *limiter
	globalSem   chan struct{}
	semMu       sync.Mutex
	resourceSem map[string]chan struct{}
	connect     func(context.Context, string, string) (net.Conn, error)
}

func New(config Config, auditOutput io.Writer) (*Server, error) {
	if config.TicketTTL <= 0 {
		config.TicketTTL = 60 * time.Second
	}
	if config.SessionTTL <= 0 {
		config.SessionTTL = 30 * time.Minute
	}
	server := &Server{
		config: config, resources: map[string]Resource{}, resourceIDs: map[string]Resource{},
		store:  newMemoryStore(config.SessionSecret),
		safety: safety.Policy{DemoMode: config.DemoMode, DemoAllowedOrigin: config.DemoTargetOrigin},
		audit:  newAuditor(auditOutput), apiLimiter: newLimiter(30, time.Minute),
		dataLimiter: newLimiter(120, time.Minute), globalSem: make(chan struct{}, 32),
		resourceSem: map[string]chan struct{}{},
	}
	for _, resource := range config.Resources {
		server.resources[resource.Origin.TupleKey()] = resource
		server.resourceIDs[resource.ID] = resource
	}
	return server, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	switch {
	case r.URL.Path == "/healthz":
		s.handleHealth(w, r)
	case r.URL.Path == "/v1/access/check":
		s.handleAccessCheck(w, r, false)
	case r.URL.Path == "/v1/launches":
		s.handleAccessCheck(w, r, true)
	case strings.HasPrefix(r.URL.Path, "/_launch/"):
		s.handleLaunch(w, r)
	default:
		s.handleProxy(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed.", requestID())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type accessRequest struct {
	InputURL string `json:"input_url"`
}

type accessResponse struct {
	Decision      string     `json:"decision"`
	Message       string     `json:"message"`
	NormalizedURL string     `json:"normalized_url,omitempty"`
	LaunchURL     string     `json:"launch_url,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

func (s *Server) handleAccessCheck(w http.ResponseWriter, r *http.Request, legacyLaunch bool) {
	started, reqID := time.Now(), requestID()
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed.", reqID)
		return
	}
	if allowed, retry := s.apiLimiter.allow("api:"+clientPrefix(r.RemoteAddr), started); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Seconds()))))
		writeJSON(w, http.StatusTooManyRequests, accessResponse{Decision: "rate_limited", Message: "Too many attempts. Wait a moment, then try again."})
		s.auditDecision(started, reqID, "anonymous", Resource{}, "access.check", r, "deny", "rate_limited", http.StatusTooManyRequests)
		return
	}
	var input accessRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxAPIRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Provide one input_url value.", reqID)
		return
	}
	normalized, err := urlpolicy.Parse(input.InputURL)
	if err != nil {
		decision := "blocked"
		message := err.Error()
		if urlpolicy.ErrorCode(err) == urlpolicy.PortNotAllowed {
			decision = "port_not_allowed"
		}
		writeJSON(w, http.StatusUnprocessableEntity, accessResponse{Decision: decision, Message: message})
		s.auditDecision(started, reqID, "anonymous", Resource{}, "access.check", r, "deny", strings.ToLower(string(urlpolicy.ErrorCode(err))), http.StatusUnprocessableEntity)
		return
	}
	resource, decision := s.findResource(normalized)
	if decision != "allowed" {
		message := "This resource has not been authorized yet."
		if decision == "port_not_allowed" {
			message = fmt.Sprintf("Port %d isn't approved for this resource.", normalized.Port)
		}
		writeJSON(w, http.StatusForbidden, accessResponse{Decision: decision, Message: message, NormalizedURL: normalized.URL})
		s.auditDecision(started, reqID, "anonymous", resource, "access.check", r, "deny", decision, http.StatusForbidden)
		return
	}
	actor := "anonymous"
	if !resource.Public {
		writeJSON(w, http.StatusForbidden, accessResponse{Decision: "resource_not_authorized", Message: "This resource is not available for public access.", NormalizedURL: normalized.URL})
		s.auditDecision(started, reqID, actor, resource, "access.check", r, "deny", "not_public", http.StatusForbidden)
		return
	}
	if !resource.AllowsPath(normalized.Path) {
		writeJSON(w, http.StatusForbidden, accessResponse{Decision: "resource_not_authorized", Message: "This path is not available for this resource.", NormalizedURL: normalized.URL})
		s.auditDecision(started, reqID, actor, resource, "access.check", r, "deny", "path_not_allowed", http.StatusForbidden)
		return
	}
	if _, err := s.safety.Resolve(r.Context(), resource.Origin.Scheme, resource.Origin.Host, resource.Origin.Port); err != nil {
		writeJSON(w, http.StatusForbidden, accessResponse{Decision: "blocked", Message: "This destination can't be opened through Permit.", NormalizedURL: normalized.URL})
		s.auditDecision(started, reqID, actor, resource, "access.check", r, "deny", "unsafe_destination", http.StatusForbidden)
		return
	}
	expires := time.Now().Add(s.config.TicketTTL)
	ticket, err := s.store.createTicket(launchState{ResourceID: resource.ID, Target: normalized, ActorID: actor, ExpiresAt: expires})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SERVICE_FAULT", "Permit couldn't open this resource right now.", reqID)
		return
	}
	response := accessResponse{Decision: "allowed", Message: "Access approved. Opening a short-lived connection.", NormalizedURL: normalized.URL, LaunchURL: s.config.PublicBaseURL + "/_launch/" + url.PathEscape(ticket), ExpiresAt: &expires}
	status := http.StatusOK
	if legacyLaunch {
		status = http.StatusCreated
	}
	writeJSON(w, status, response)
	s.auditDecision(started, reqID, actor, resource, "access.check", r, "allow", "public_grant_active", status)
}

func (s *Server) findResource(target urlpolicy.Normalized) (Resource, string) {
	if resource, ok := s.resources[target.TupleKey()]; ok {
		return resource, "allowed"
	}
	for _, resource := range s.resources {
		if resource.Origin.Scheme == target.Scheme && resource.Origin.Host == target.Host {
			return resource, "port_not_allowed"
		}
	}
	return Resource{}, "resource_not_authorized"
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	started, reqID := time.Now(), requestID()
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed.", reqID)
		return
	}
	token, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/_launch/"))
	if err != nil || token == "" || strings.Contains(token, "/") {
		writeError(w, http.StatusBadRequest, "INVALID_LAUNCH", "This launch link is invalid.", reqID)
		return
	}
	state, err := s.store.consumeTicket(token, time.Now())
	if err != nil {
		writeError(w, http.StatusGone, "LAUNCH_EXPIRED", "This launch link has expired or was already used.", reqID)
		return
	}
	resource, ok := s.resourceIDs[state.ResourceID]
	if !ok || (!resource.Public && state.ActorID == "anonymous") {
		writeError(w, http.StatusForbidden, "RESOURCE_NOT_AUTHORIZED", "This resource is not available.", reqID)
		return
	}
	if _, err := s.safety.Resolve(r.Context(), resource.Origin.Scheme, resource.Origin.Host, resource.Origin.Port); err != nil {
		writeError(w, http.StatusForbidden, "DESTINATION_BLOCKED", "This destination can't be opened through Permit.", reqID)
		s.auditDecision(started, reqID, state.ActorID, resource, "launch.consume", r, "deny", "unsafe_destination", http.StatusForbidden)
		return
	}
	sessionExpiry := time.Now().Add(s.config.SessionTTL)
	session, err := s.store.createSession(sessionState{ResourceID: resource.ID, ActorID: state.ActorID, ExpiresAt: sessionExpiry})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SERVICE_FAULT", "Permit couldn't open this resource right now.", reqID)
		return
	}
	secure := strings.HasPrefix(s.config.PublicBaseURL, "https://")
	http.SetCookie(w, &http.Cookie{Name: s.sessionCookieName(), Value: session, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int(s.config.SessionTTL.Seconds())})
	destination := (&url.URL{Path: state.Target.Path, RawQuery: state.Target.RawQuery, Fragment: state.Target.Fragment}).String()
	http.Redirect(w, r, destination, http.StatusSeeOther)
	s.auditDecision(started, reqID, state.ActorID, resource, "launch.consume", r, "allow", "ticket_consumed", http.StatusSeeOther)
}

func (s *Server) sessionCookieName() string {
	if strings.HasPrefix(s.config.PublicBaseURL, "https://") {
		return "__Host-aa_session"
	}
	return "aa_demo_session"
}

func (s *Server) auditDecision(start time.Time, requestID, actor string, resource Resource, action string, r *http.Request, decision, reason string, status int) {
	s.audit.write(auditEvent{OccurredAt: time.Now().UTC(), RequestID: requestID, ActorID: actor, ResourceID: resource.ID, Action: action, Method: r.Method, PathHash: pathHash(r.URL.Path), Decision: decision, Reason: reason, StatusCode: status, DurationMS: time.Since(start).Milliseconds(), ClientNet: clientPrefix(r.RemoteAddr)})
}

func requestID() string {
	token, err := randomToken()
	if err != nil {
		return "req_unknown"
	}
	return "req_" + token[:16]
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message, requestID string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": requestID}})
}

func (s *Server) acquire(resourceID string) (func(), bool) {
	select {
	case s.globalSem <- struct{}{}:
	default:
		return nil, false
	}
	s.semMu.Lock()
	resourceLimit := s.resourceSem[resourceID]
	if resourceLimit == nil {
		resourceLimit = make(chan struct{}, 8)
		s.resourceSem[resourceID] = resourceLimit
	}
	s.semMu.Unlock()
	select {
	case resourceLimit <- struct{}{}:
		return func() { <-resourceLimit; <-s.globalSem }, true
	default:
		<-s.globalSem
		return nil, false
	}
}

func (s *Server) actorSession(r *http.Request) (sessionState, Resource, error) {
	cookie, err := r.Cookie(s.sessionCookieName())
	if err != nil {
		return sessionState{}, Resource{}, err
	}
	state, err := s.store.session(cookie.Value, time.Now())
	if err != nil {
		return sessionState{}, Resource{}, err
	}
	resource, ok := s.resourceIDs[state.ResourceID]
	if !ok || (!resource.Public && state.ActorID == "anonymous") {
		return sessionState{}, Resource{}, errors.New("resource grant is no longer active")
	}
	return state, resource, nil
}
