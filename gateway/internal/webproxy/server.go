package webproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"permit-gateway/internal/safety"
	"permit-gateway/internal/stats"

	"golang.org/x/net/html/charset"
)

const (
	browsePrefix             = "/browse/"
	socketPrefix             = "/socket/"
	tunnelPrefix             = "/tunnel/"
	virtualOriginParam       = "__owu_origin_v1"
	maxRequestBodyBytes      = 64 << 20
	maxRewriteBodyBytes      = 16 << 20
	defaultTransportPoolSize = 64
	defaultMediaCacheMaxAge  = 15 * time.Minute
	manifestCacheMaxAge      = 60 * time.Second
)

type Config struct {
	ListenAddr        string
	DemoAllowedOrigin string
	TunnelKey         string
	TunnelResources   map[string]TCPResource
	TransportPoolSize int
	MediaCacheMaxAge  time.Duration
	StatsFile         string
}

func LoadConfig() Config {
	listen := strings.TrimSpace(os.Getenv("OWU_PROXY_LISTEN_ADDR"))
	if listen == "" {
		listen = "127.0.0.1:3211"
	}
	resources, err := parseTCPResources(os.Getenv("OWU_TCP_RESOURCES"))
	if err != nil {
		panic("invalid OWU_TCP_RESOURCES: " + err.Error())
	}
	return Config{
		ListenAddr:        listen,
		TunnelKey:         strings.TrimSpace(os.Getenv("OWU_TUNNEL_KEY")),
		TunnelResources:   resources,
		TransportPoolSize: positiveIntEnv("OWU_TRANSPORT_POOL_SIZE", defaultTransportPoolSize),
		MediaCacheMaxAge:  durationEnv("OWU_MEDIA_CACHE_MAX_AGE", defaultMediaCacheMaxAge),
		StatsFile:         strings.TrimSpace(os.Getenv("OWU_STATS_FILE")),
	}
}

type Server struct {
	safety            safety.Policy
	demoAllowedOrigin string
	tunnelKey         string
	tunnelResources   map[string]TCPResource
	transportMu       sync.Mutex
	transports        map[string]*transportEntry
	transportSequence uint64
	transportPoolSize int
	mediaCacheMaxAge  time.Duration
	stats             *stats.Recorder
}

type transportEntry struct {
	transport *http.Transport
	lastUsed  uint64
}

func New(config Config) *Server {
	transportPoolSize := config.TransportPoolSize
	if transportPoolSize == 0 {
		transportPoolSize = defaultTransportPoolSize
	}
	if transportPoolSize < 1 {
		transportPoolSize = 1
	}
	mediaCacheMaxAge := config.MediaCacheMaxAge
	if mediaCacheMaxAge == 0 {
		mediaCacheMaxAge = defaultMediaCacheMaxAge
	}
	var recorder *stats.Recorder
	if config.StatsFile != "" {
		created, err := stats.New(config.StatsFile)
		if err != nil {
			log.Printf("OWU usage statistics disabled: %v", err)
		} else {
			created.Start(30 * time.Second)
			recorder = created
		}
	}
	return &Server{
		safety:            safePolicy(config.DemoAllowedOrigin),
		demoAllowedOrigin: config.DemoAllowedOrigin,
		tunnelKey:         config.TunnelKey,
		tunnelResources:   config.TunnelResources,
		transports:        make(map[string]*transportEntry),
		transportPoolSize: transportPoolSize,
		mediaCacheMaxAge:  mediaCacheMaxAge,
		stats:             recorder,
	}
}

func positiveIntEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 4096 {
		panic("invalid " + name + ": expected an integer from 1 to 4096")
	}
	return value
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	if strings.EqualFold(raw, "off") || raw == "0" {
		return -1
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 || value > 24*time.Hour {
		panic("invalid " + name + ": expected off or a duration up to 24h")
	}
	return value
}

func safePolicy(demoOrigin string) safety.Policy {
	return safety.Policy{DemoMode: demoOrigin != "", DemoAllowedOrigin: demoOrigin}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.stats == nil {
		s.serveHTTP(w, r)
		return
	}
	counter := &countingResponseWriter{ResponseWriter: w}
	s.serveHTTP(counter, r)
	traffic := counter.bytes
	if r.ContentLength > 0 {
		traffic += uint64(r.ContentLength)
	}
	s.recordTraffic(traffic)
}

// countingResponseWriter counts response bytes written to the client so the
// anonymous traffic metric covers proxied pages, media, sockets, and API calls.
type countingResponseWriter struct {
	http.ResponseWriter
	bytes uint64
}

func (c *countingResponseWriter) Write(payload []byte) (int, error) {
	written, err := c.ResponseWriter.Write(payload)
	c.bytes += uint64(written)
	return written, err
}

// Unwrap lets http.ResponseController (SSE flushing, WebSocket hijacking) reach
// the underlying writer.
func (c *countingResponseWriter) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	virtualTarget, virtualToken, virtual := parseVirtualTarget(r.URL)
	switch {
	case virtual:
		if r.Method == http.MethodConnect || r.Method == http.MethodTrace {
			writeError(w, http.StatusMethodNotAllowed, "This request method is not supported.")
			return
		}
		s.proxyHTTP(w, r, virtualTarget, virtualToken)
	case r.URL.Path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case r.URL.Path == "/stats":
		s.handleStatsPage(w, r)
	case r.URL.Path == "/stats/api":
		s.handleStatsAPI(w, r)
	case strings.HasPrefix(r.URL.Path, browsePrefix):
		s.handleBrowse(w, r)
	case strings.HasPrefix(r.URL.Path, socketPrefix):
		s.handleSocket(w, r)
	case strings.HasPrefix(r.URL.Path, tunnelPrefix):
		s.handleTunnel(w, r)
	default:
		s.handleRefererFallback(w, r)
	}
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect || r.Method == http.MethodTrace {
		writeError(w, http.StatusMethodNotAllowed, "This request method is not supported.")
		return
	}
	target, token, err := parseProxyTarget(r.URL, browsePrefix, map[string]bool{"http": true, "https": true})
	if err != nil {
		writeError(w, http.StatusBadRequest, "The proxy address is invalid.")
		return
	}
	s.proxyHTTP(w, r, target, token)
}

func (s *Server) handleRefererFallback(w http.ResponseWriter, r *http.Request) {
	base, ok := proxiedRefererTarget(r.Referer())
	if !ok {
		writeError(w, http.StatusNotFound, "Not found.")
		return
	}
	target := &url.URL{Scheme: base.Scheme, Host: base.Host, Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}
	// Canonicalize root-relative navigations and module/resource requests back
	// under /browse/{origin}. Keeping the proxy token in the visible URL makes
	// subsequent relative requests deterministic even after JS location changes.
	http.Redirect(w, r, proxyURL(target), http.StatusTemporaryRedirect)
}

func (s *Server) proxyHTTP(w http.ResponseWriter, r *http.Request, target *url.URL, token string) {
	if r.ContentLength > maxRequestBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "The request body is too large.")
		return
	}
	// Bound connection establishment and response headers, but do not impose a
	// two-minute deadline on a successfully established SSE stream or download.
	validationCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	if err := s.validateTarget(validationCtx, target); err != nil {
		cancel()
		writeError(w, http.StatusForbidden, "This destination is not a public Internet address.")
		return
	}
	cancel()
	s.recordUse(r, target.Hostname(), target.Path)
	ctx := r.Context()

	body, err := outboundRequestBody(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "The request body could not be read.")
		return
	}
	outbound, err := http.NewRequestWithContext(ctx, r.Method, target.String(), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "The upstream request could not be created.")
		return
	}
	if body != nil {
		// MaxBytesReader intentionally hides the underlying size. Preserve a known
		// inbound length so fixed-length POST/PUT/PATCH bodies do not become
		// chunked while retaining chunked semantics when ContentLength is unknown.
		outbound.ContentLength = r.ContentLength
	}
	copyRequestHeaders(outbound.Header, r.Header)
	outbound.Host = target.Host
	setTargetCookies(outbound, r, token)
	rewriteRequestOrigin(outbound, r)

	transport := s.transportFor(target)
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(outbound)
	if err != nil {
		writeError(w, http.StatusBadGateway, "The destination did not respond.")
		return
	}
	defer response.Body.Close()

	if err := writeProxyResponseWithCache(w, r, response, target, token, s.mediaCacheMaxAge); err != nil {
		writeError(w, http.StatusBadGateway, "The destination response could not be processed.")
	}
}

func outboundRequestBody(w http.ResponseWriter, r *http.Request) (io.Reader, error) {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return nil, nil
	}

	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	if r.ContentLength >= 0 || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return body, nil
	}

	// Unknown-length GET and HEAD requests need one byte of lookahead. Forward a
	// real chunked body without data loss, but turn an empty chunked body into a
	// nil reader so Go does not emit request-body framing. Poki's edge renderer,
	// among others, rejects an otherwise ordinary GET carrying empty framing.
	var first [1]byte
	if _, err := io.ReadFull(body, first[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	return io.MultiReader(bytes.NewReader(first[:]), body), nil
}

func (s *Server) handleSocket(w http.ResponseWriter, r *http.Request) {
	target, _, err := parseProxyTarget(r.URL, socketPrefix, map[string]bool{"ws": true, "wss": true})
	if err != nil {
		writeError(w, http.StatusBadRequest, "The WebSocket proxy address is invalid.")
		return
	}
	mapped := *target
	if mapped.Scheme == "ws" {
		mapped.Scheme = "http"
	} else {
		mapped.Scheme = "https"
	}
	if err := s.validateTarget(r.Context(), &mapped); err != nil {
		writeError(w, http.StatusForbidden, "This destination is not a public Internet address.")
		return
	}
	s.recordUse(r, mapped.Hostname(), mapped.Path)

	transport := s.transportFor(&mapped)
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.Out.URL = &url.URL{Scheme: mapped.Scheme, Host: mapped.Host, Path: mapped.Path, RawPath: mapped.RawPath, RawQuery: mapped.RawQuery}
			request.Out.Host = mapped.Host
			request.Out.Header = make(http.Header)
			copyRequestHeaders(request.Out.Header, request.In.Header)
			setTargetCookies(request.Out, request.In, encodeOrigin(&mapped))
			rewriteRequestOrigin(request.Out, request.In)
			request.Out.Header.Set("Connection", "Upgrade")
			request.Out.Header.Set("Upgrade", "websocket")
		},
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			writeError(writer, http.StatusBadGateway, "The WebSocket destination did not respond.")
		},
		FlushInterval: 100 * time.Millisecond,
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) transportFor(target *url.URL) *http.Transport {
	key := strings.ToLower(target.Scheme) + "://" + strings.ToLower(target.Host)
	s.transportMu.Lock()
	s.transportSequence++
	if entry := s.transports[key]; entry != nil {
		entry.lastUsed = s.transportSequence
		transport := entry.transport
		s.transportMu.Unlock()
		return transport
	}

	var evicted *http.Transport
	if len(s.transports) >= s.transportPoolSize {
		var oldestKey string
		oldestSequence := ^uint64(0)
		for candidateKey, entry := range s.transports {
			if entry.lastUsed < oldestSequence {
				oldestKey = candidateKey
				oldestSequence = entry.lastUsed
			}
		}
		if oldestKey != "" {
			evicted = s.transports[oldestKey].transport
			delete(s.transports, oldestKey)
		}
	}

	transport := s.newTransport(target)
	s.transports[key] = &transportEntry{transport: transport, lastUsed: s.transportSequence}
	s.transportMu.Unlock()
	if evicted != nil {
		// CloseIdleConnections leaves in-flight requests alone. Once the bounded
		// per-origin LRU evicts an entry, its idle sockets no longer consume file
		// descriptors while active video streams are allowed to finish.
		evicted.CloseIdleConnections()
	}
	return transport
}

func (s *Server) newTransport(target *url.URL) *http.Transport {
	host, port := target.Hostname(), targetPort(target)
	return &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if s.isDemoTarget(target) {
				requestedHost, requestedPort, err := net.SplitHostPort(address)
				if err != nil || !strings.EqualFold(strings.TrimSuffix(requestedHost, "."), host) || requestedPort != strconv.Itoa(port) {
					return nil, errors.New("upstream dial target did not match the test destination")
				}
				dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
				return dialer.DialContext(ctx, network, address)
			}
			return s.safety.DialContext(ctx, network, address, target.Scheme, host, port)
		},
	}
}

func (s *Server) validateTarget(ctx context.Context, target *url.URL) error {
	if s.isDemoTarget(target) {
		return nil
	}
	_, err := s.safety.Resolve(ctx, target.Scheme, target.Hostname(), targetPort(target))
	return err
}

func (s *Server) isDemoTarget(target *url.URL) bool {
	if s.demoAllowedOrigin == "" {
		return false
	}
	origin := target.Scheme + "://" + target.Host
	return origin == s.demoAllowedOrigin
}

func parseProxyTarget(requestURL *url.URL, prefix string, schemes map[string]bool) (*url.URL, string, error) {
	remainder := strings.TrimPrefix(requestURL.Path, prefix)
	if remainder == requestURL.Path || remainder == "" {
		return nil, "", errors.New("missing proxy token")
	}
	token, path, found := strings.Cut(remainder, "/")
	if !found {
		path = ""
	}
	origin, err := decodeOrigin(token, schemes)
	if err != nil {
		return nil, "", err
	}
	origin.Path = "/" + path
	if requestURL.RawPath != "" {
		rawRemainder := strings.TrimPrefix(requestURL.RawPath, prefix+token)
		if strings.HasPrefix(rawRemainder, "/") {
			origin.RawPath = rawRemainder
		}
	}
	origin.RawQuery = requestURL.RawQuery
	return origin, token, nil
}

func decodeOrigin(token string, schemes map[string]bool) (*url.URL, error) {
	if len(token) == 0 || len(token) > 2048 {
		return nil, errors.New("invalid token length")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) > 1536 {
		return nil, errors.New("invalid origin token")
	}
	origin, err := url.Parse(string(decoded))
	if err != nil || !schemes[strings.ToLower(origin.Scheme)] || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("invalid origin")
	}
	if targetPort(origin) < 1 || targetPort(origin) > 65535 {
		return nil, errors.New("invalid port")
	}
	canonicalizeOrigin(origin)
	return origin, nil
}

func encodeOrigin(target *url.URL) string {
	copy := *target
	canonicalizeOrigin(&copy)
	origin := copy.Scheme + "://" + copy.Host
	return base64.RawURLEncoding.EncodeToString([]byte(origin))
}

func canonicalizeOrigin(target *url.URL) {
	target.Scheme = strings.ToLower(target.Scheme)
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	port := target.Port()
	if (target.Scheme == "https" || target.Scheme == "wss") && port == "443" || (target.Scheme == "http" || target.Scheme == "ws") && port == "80" {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	target.Host = host
}

func targetPort(target *url.URL) int {
	if raw := target.Port(); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil {
			return 0
		}
		return port
	}
	if target.Scheme == "https" || target.Scheme == "wss" {
		return 443
	}
	return 80
}

func proxyURL(target *url.URL) string {
	path := target.EscapedPath()
	if path == "" {
		path = "/"
	}
	result := browsePrefix + encodeOrigin(target) + path
	if target.RawQuery != "" {
		result += "?" + target.RawQuery
	}
	if target.Fragment != "" {
		result += "#" + url.PathEscape(target.Fragment)
	}
	return result
}

func copyRequestHeaders(destination, source http.Header) {
	for name, values := range source {
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "cookie" || lower == "host" || lower == "accept-encoding" || lower == "connection" || lower == "proxy-connection" || lower == "upgrade" || lower == "te" || lower == "trailer" || lower == "transfer-encoding" || strings.HasPrefix(lower, "x-forwarded-") || strings.HasPrefix(lower, "proxy-") || strings.HasPrefix(lower, "sec-fetch-") {
			continue
		}
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func cookiePrefix(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "owu_" + hex.EncodeToString(sum[:6]) + "_"
}

func setTargetCookies(outbound, incoming *http.Request, token string) {
	outbound.Header.Del("Cookie")
	prefix := cookiePrefix(token)
	for _, cookie := range incoming.Cookies() {
		if strings.HasPrefix(cookie.Name, prefix) {
			outbound.AddCookie(&http.Cookie{Name: strings.TrimPrefix(cookie.Name, prefix), Value: cookie.Value})
		}
	}
}

func rewriteRequestOrigin(outbound, incoming *http.Request) {
	source, ok := proxiedRefererTarget(incoming.Referer())
	if !ok {
		outbound.Header.Del("Origin")
		outbound.Header.Del("Referer")
		return
	}
	if incoming.Header.Get("Origin") != "" {
		outbound.Header.Set("Origin", source.Scheme+"://"+source.Host)
	}
	outbound.Header.Set("Referer", source.String())
}

func proxiedRefererTarget(value string) (*url.URL, bool) {
	referer, err := url.Parse(value)
	if err != nil {
		return nil, false
	}
	if strings.HasPrefix(referer.Path, browsePrefix) {
		target, _, err := parseProxyTarget(referer, browsePrefix, map[string]bool{"http": true, "https": true})
		return target, err == nil
	}
	target, _, ok := parseVirtualTarget(referer)
	return target, ok
}

func parseVirtualTarget(requestURL *url.URL) (*url.URL, string, bool) {
	parts := strings.Split(requestURL.RawQuery, "&")
	for index := len(parts) - 1; index >= 0; index-- {
		rawKey, rawValue, found := strings.Cut(parts[index], "=")
		if !found {
			continue
		}
		key, err := url.QueryUnescape(rawKey)
		if err != nil || key != virtualOriginParam {
			continue
		}
		token, err := url.QueryUnescape(rawValue)
		if err != nil {
			continue
		}
		target, err := decodeOrigin(token, map[string]bool{"http": true, "https": true})
		if err != nil {
			continue
		}
		remaining := make([]string, 0, len(parts)-1)
		remaining = append(remaining, parts[:index]...)
		remaining = append(remaining, parts[index+1:]...)
		target.Path = requestURL.Path
		target.RawPath = requestURL.RawPath
		target.RawQuery = strings.Join(remaining, "&")
		target.Fragment = requestURL.Fragment
		return target, token, true
	}
	return nil, "", false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message}})
}

// Close stops background statistic flushing.
func (s *Server) Close() {
	if s.stats != nil {
		s.stats.Close()
	}
}

// recordUse marks anonymous activity. Subresource requests still identify an
// active visitor but do not inflate the usage totals or the site ranking.
func (s *Server) recordUse(r *http.Request, site, path string) {
	if s.stats == nil {
		return
	}
	s.stats.Record(s.stats.VisitorID(r.RemoteAddr), stats.NormalizeSite(site), !hasMediaExtension(path))
}

func (s *Server) recordTraffic(bytes uint64) {
	if s.stats != nil {
		s.stats.AddTraffic(bytes)
	}
}

func (s *Server) handleStatsAPI(w http.ResponseWriter, r *http.Request) {
	if s.stats == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "message": "Usage statistics are not configured."})
		return
	}
	snapshot := s.stats.Snapshot()
	type siteCount struct {
		Site string `json:"site"`
		Uses int64  `json:"uses"`
	}
	top := make([]siteCount, 0, len(snapshot.Sites))
	for site, uses := range snapshot.Sites {
		top = append(top, siteCount{Site: site, Uses: uses})
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].Uses != top[j].Uses {
			return top[i].Uses > top[j].Uses
		}
		return top[i].Site < top[j].Site
	})
	if len(top) > 10 {
		top = top[:10]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       true,
		"since":         snapshot.Since,
		"updatedAt":     snapshot.UpdatedAt,
		"visitorsTotal": len(snapshot.Visitors),
		"visitorsToday": s.stats.VisitorsToday(snapshot),
		"usesTotal":     snapshot.UsesTotal,
		"usesToday":     snapshot.UsesToday,
		"trafficTotal":  snapshot.TrafficTotal,
		"trafficToday":  snapshot.TrafficToday,
		"topSites":      top,
	})
}

func (s *Server) handleStatsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, statsPageHTML)
}

const statsPageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>OWU · Usage Statistics</title>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body {
    margin: 0; min-height: 100vh; font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "PingFang SC", "Microsoft YaHei", sans-serif;
    background: radial-gradient(1200px 600px at 15% -10%, rgba(56,132,255,.16), transparent 60%), radial-gradient(1000px 500px at 110% 20%, rgba(168,85,247,.14), transparent 55%), #0b0f17;
    color: #e7ecf5; display: flex; align-items: center; justify-content: center; padding: 32px 16px;
  }
  .card { width: 100%; max-width: 720px; background: rgba(255,255,255,.045); border: 1px solid rgba(255,255,255,.09); border-radius: 24px; padding: 28px; backdrop-filter: blur(18px); box-shadow: 0 24px 60px rgba(0,0,0,.35); }
  header { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 20px; }
  h1 { font-size: 20px; margin: 0; letter-spacing: .02em; }
  .updated { font-size: 12px; color: #8b96ab; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 14px; }
  .metric { background: rgba(255,255,255,.05); border: 1px solid rgba(255,255,255,.08); border-radius: 16px; padding: 18px; }
  .metric .label { font-size: 13px; color: #9aa6bd; }
  .metric .value { font-size: 30px; font-weight: 650; margin-top: 6px; font-variant-numeric: tabular-nums; }
  .metric .sub { font-size: 12px; color: #6f7c94; margin-top: 4px; }
  h2 { font-size: 15px; margin: 26px 0 12px; color: #c6d0e2; }
  ol { margin: 0; padding: 0; list-style: none; }
  li { display: flex; align-items: center; gap: 12px; padding: 10px 14px; border-radius: 12px; background: rgba(255,255,255,.035); border: 1px solid rgba(255,255,255,.06); margin-bottom: 8px; }
  .rank { width: 26px; height: 26px; border-radius: 8px; background: rgba(56,132,255,.18); color: #7fb0ff; display: grid; place-items: center; font-size: 13px; font-weight: 650; flex: none; }
  .site { flex: 1; font-size: 14px; word-break: break-all; }
  .uses { font-size: 13px; color: #8b96ab; font-variant-numeric: tabular-nums; }
  .empty { color: #6f7c94; font-size: 14px; padding: 14px 4px; }
  footer { margin-top: 22px; font-size: 12px; color: #6f7c94; line-height: 1.7; border-top: 1px solid rgba(255,255,255,.07); padding-top: 16px; }
  a { color: #7fb0ff; text-decoration: none; }
</style>
</head>
<body>
<main class="card">
  <header>
    <h1>OWU Usage Statistics</h1>
    <span class="updated" id="updated">Loading…</span>
  </header>
  <div class="grid">
    <section class="metric"><div class="label">Visitors (total)</div><div class="value" id="visitorsTotal">&ndash;</div><div class="sub" id="visitorsToday">&ndash; today</div></section>
    <section class="metric"><div class="label">Uses (total)</div><div class="value" id="usesTotal">&ndash;</div><div class="sub" id="usesToday">&ndash; today</div></section>
    <section class="metric"><div class="label">Traffic (total)</div><div class="value" id="trafficTotal">&ndash;</div><div class="sub" id="trafficToday">&ndash; today</div></section>
    <section class="metric"><div class="label">Counting since</div><div class="value" id="since" style="font-size:20px;margin-top:14px">&ndash;</div><div class="sub">Anonymous</div></section>
  </div>
  <h2>Most visited websites</h2>
  <ol id="sites"><li class="empty">No data yet</li></ol>
  <footer>
    Statistics are <b>anonymous</b>: visitors are identified only by an irreversible hash and no IP or other
    identifying data is stored; destination websites are recorded by domain and usage count only.<br/>
    <a href="/">← Back to OWU</a>
  </footer>
</main>
<script>
const fmt = new Intl.NumberFormat("en-US");
const byId = (id) => document.getElementById(id);
function formatBytes(value) {
  if (value >= 1073741824) return (value / 1073741824).toFixed(2) + " GB";
  if (value >= 1048576) return (value / 1048576).toFixed(1) + " MB";
  if (value >= 1024) return (value / 1024).toFixed(0) + " KB";
  return value + " B";
}
function fmtTime(value) {
  if (!value) return "&ndash;";
  const date = new Date(value);
  return isNaN(date) ? "&ndash;" : date.toLocaleString("en-US", { hour12: false });
}
async function refresh() {
  try {
    const response = await fetch("/stats/api", { cache: "no-store" });
    const data = await response.json();
    if (!data.enabled) { byId("updated").textContent = "Statistics not enabled"; return; }
    byId("visitorsTotal").textContent = fmt.format(data.visitorsTotal || 0);
    byId("visitorsToday").textContent = fmt.format(data.visitorsToday || 0) + " today";
    byId("usesTotal").textContent = fmt.format(data.usesTotal || 0);
    byId("usesToday").textContent = fmt.format(data.usesToday || 0) + " today";
    byId("trafficTotal").textContent = formatBytes(data.trafficTotal || 0);
    byId("trafficToday").textContent = formatBytes(data.trafficToday || 0) + " today";
    byId("since").textContent = fmtTime(data.since);
    byId("updated").textContent = "Updated at " + fmtTime(data.updatedAt);
    const list = byId("sites");
    if (!data.topSites || data.topSites.length === 0) {
      list.innerHTML = '<li class="empty">No data yet</li>';
      return;
    }
    list.innerHTML = "";
    data.topSites.forEach((entry, index) => {
      const item = document.createElement("li");
      const rank = document.createElement("span");
      rank.className = "rank";
      rank.textContent = String(index + 1);
      const site = document.createElement("span");
      site.className = "site";
      site.textContent = entry.site;
      const uses = document.createElement("span");
      uses.className = "uses";
      uses.textContent = fmt.format(entry.uses) + " uses";
      item.append(rank, site, uses);
      list.appendChild(item);
    });
  } catch {
    byId("updated").textContent = "Failed to load — retrying";
  }
}
refresh();
setInterval(refresh, 10000);
</script>
</body>
</html>`

func writeProxyResponse(w http.ResponseWriter, incoming *http.Request, response *http.Response, target *url.URL, token string) error {
	return writeProxyResponseWithCache(w, incoming, response, target, token, defaultMediaCacheMaxAge)
}

func writeProxyResponseWithCache(w http.ResponseWriter, incoming *http.Request, response *http.Response, target *url.URL, token string, cacheMaxAge time.Duration) error {
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	partialResponse := response.StatusCode == http.StatusPartialContent || response.Header.Get("Content-Range") != ""
	rewriteHTMLBody := !partialResponse && (strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml+xml"))
	rewriteCSSBody := !partialResponse && strings.Contains(contentType, "text/css")
	rewriteHLSBody := !partialResponse && !rewriteHTMLBody && isHLSManifestContent(contentType, target.Path)
	rewriteDASHBody := !partialResponse && !rewriteHTMLBody && isDASHManifestContent(contentType, target.Path)
	rewrittenResponse := rewriteHTMLBody || rewriteCSSBody || rewriteHLSBody || rewriteDASHBody
	// Rewritten CSS is deterministic for the target URL/token and can therefore
	// use the guarded public-static cache path. HTML contains live page/session
	// state and remains uncacheable even when the origin labels it public.
	cache := mediaCacheDecisionFor(incoming, response, target, token, rewriteHTMLBody, cacheMaxAge)

	copyResponseHeaders(w.Header(), response.Header)
	rewriteResponseHeaders(w.Header(), response, target, token, incoming.Header.Get("X-Forwarded-Proto") == "https", cache)
	if partialResponse || strings.HasPrefix(contentType, "text/event-stream") || (cache.sharedMaxAge == 0 && isStreamMediaContent(contentType, target.Path)) {
		// Disable reverse-proxy buffering for byte ranges and event streams. This
		// preserves seek latency for Bilibili/Douyin media and immediate SSE delivery.
		w.Header().Set("X-Accel-Buffering", "no")
	}

	if incoming.Method == http.MethodHead || response.Body == nil {
		if rewrittenResponse {
			w.Header().Del("Content-Length")
			w.Header().Del("ETag")
		} else {
			setStreamingContentLength(w.Header(), response)
		}
		w.WriteHeader(response.StatusCode)
		return nil
	}
	if rewrittenResponse {
		body, err := io.ReadAll(io.LimitReader(response.Body, maxRewriteBodyBytes+1))
		if err != nil || len(body) > maxRewriteBodyBytes {
			return errors.New("rewritable response body is too large")
		}
		switch {
		case rewriteHTMLBody:
			body, err = decodeHTMLToUTF8(body, response.Header.Get("Content-Type"))
			if err != nil {
				return err
			}
			body, err = rewriteHTML(body, target)
			if err != nil {
				return err
			}
			w.Header().Set("Content-Type", utf8ContentType(response.Header.Get("Content-Type")))
			w.Header().Set("Content-Security-Policy", proxyContentSecurityPolicy)
		case rewriteCSSBody:
			body = rewriteCSS(body, target)
		case rewriteHLSBody:
			body = rewriteHLSManifest(body, target)
		case rewriteDASHBody:
			body, err = rewriteDASHManifest(body, target)
			if err != nil {
				return err
			}
		}
		w.Header().Del("Content-Encoding")
		w.Header().Del("ETag")
		w.Header().Del("Content-MD5")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(response.StatusCode)
		_, err = w.Write(body)
		return err
	}

	setStreamingContentLength(w.Header(), response)
	w.WriteHeader(response.StatusCode)
	return copyProxyBody(w, response.Body, partialResponse || strings.HasPrefix(contentType, "text/event-stream"))
}

var proxyCopyBufferPool = sync.Pool{New: func() any {
	buffer := make([]byte, 128<<10)
	return &buffer
}}

type flushingWriter struct {
	writer     io.Writer
	controller *http.ResponseController
}

func (writer flushingWriter) Write(payload []byte) (int, error) {
	written, err := writer.writer.Write(payload)
	if written > 0 {
		if flushErr := writer.controller.Flush(); err == nil && flushErr != nil && !errors.Is(flushErr, http.ErrNotSupported) {
			err = flushErr
		}
	}
	return written, err
}

func copyProxyBody(w http.ResponseWriter, body io.Reader, flush bool) error {
	buffer := proxyCopyBufferPool.Get().(*[]byte)
	defer proxyCopyBufferPool.Put(buffer)
	var destination io.Writer = w
	if flush {
		destination = flushingWriter{writer: w, controller: http.NewResponseController(w)}
	}
	_, err := io.CopyBuffer(destination, body, *buffer)
	return err
}

func setStreamingContentLength(headers http.Header, response *http.Response) {
	if response.Uncompressed || response.ContentLength < 0 {
		headers.Del("Content-Length")
		return
	}
	if response.ContentLength > 0 {
		headers.Set("Content-Length", strconv.FormatInt(response.ContentLength, 10))
		return
	}
	// Preserve an explicit zero from the origin, but do not infer Content-Length:
	// 0 for synthetic responses whose body length was not populated.
	if response.Header.Get("Content-Length") == "" {
		headers.Del("Content-Length")
	}
}

func decodeHTMLToUTF8(body []byte, contentType string) ([]byte, error) {
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return nil, err
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maxRewriteBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(decoded) > maxRewriteBodyBytes {
		return nil, errors.New("decoded HTML response body is too large")
	}
	return decoded, nil
}

func utf8ContentType(contentType string) string {
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return "text/html; charset=utf-8"
	}
	parameters["charset"] = "utf-8"
	return mime.FormatMediaType(mediaType, parameters)
}

const proxyContentSecurityPolicy = "default-src 'self' data: blob:; script-src 'self' 'unsafe-inline' 'unsafe-eval' data: blob:; style-src 'self' 'unsafe-inline' data: blob:; img-src 'self' data: blob:; media-src 'self' data: blob:; font-src 'self' data: blob:; connect-src 'self' data: blob:; frame-src 'self' data: blob:; form-action 'self'; object-src 'none'; base-uri 'self'; navigate-to 'self'"

func copyResponseHeaders(destination, source http.Header) {
	blocked := map[string]bool{
		"connection": true, "keep-alive": true, "proxy-authenticate": true, "proxy-authorization": true,
		"te": true, "trailer": true, "transfer-encoding": true, "upgrade": true, "set-cookie": true,
		"content-security-policy": true, "content-security-policy-report-only": true, "x-frame-options": true,
		"cross-origin-opener-policy": true, "cross-origin-embedder-policy": true, "cross-origin-resource-policy": true,
		"origin-agent-cluster": true, "clear-site-data": true, "strict-transport-security": true, "link": true,
		"service-worker-allowed": true, "alt-svc": true,
		"www-authenticate": true, "access-control-allow-origin": true, "access-control-allow-credentials": true,
	}
	for name, values := range source {
		lower := strings.ToLower(name)
		if blocked[lower] || lower == "x-owu-cache" || strings.HasPrefix(lower, "x-accel-") || lower == "x-sendfile" {
			continue
		}
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

type mediaCacheDecision struct {
	browserMaxAge int
	sharedMaxAge  int
	immutable     bool
}

type cacheControlDirectives struct {
	public    bool
	private   bool
	noStore   bool
	noCache   bool
	immutable bool
	invalid   bool
	maxAge    *int64
	sharedAge *int64
}

func mediaCacheDecisionFor(incoming *http.Request, response *http.Response, target *url.URL, token string, unsafeRewrite bool, maxAge time.Duration) mediaCacheDecision {
	if maxAge <= 0 || unsafeRewrite || (incoming.Method != http.MethodGet && incoming.Method != http.MethodHead) {
		return mediaCacheDecision{}
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return mediaCacheDecision{}
	}
	if !isMediaOrStaticContent(response.Header.Get("Content-Type"), target.Path) {
		return mediaCacheDecision{}
	}
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Disposition")), "attachment") {
		return mediaCacheDecision{}
	}
	if hasTargetCookie(incoming, token) || len(response.Header.Values("Set-Cookie")) != 0 || requestBypassesCache(incoming.Header) {
		return mediaCacheDecision{}
	}

	directives := parseCacheControl(response.Header.Values("Cache-Control"))
	if directives.invalid || directives.private || directives.noStore || directives.noCache {
		return mediaCacheDecision{}
	}
	sharedTTL, sharedOK := sharedCacheTTL(directives, maxAge)
	browserTTL, browserOK := browserCacheTTL(directives, maxAge)
	if !sharedOK && !browserOK {
		return mediaCacheDecision{}
	}
	if isMediaManifest(response.Header.Get("Content-Type"), target.Path) {
		if sharedTTL > manifestCacheMaxAge {
			sharedTTL = manifestCacheMaxAge
		}
		if browserTTL > manifestCacheMaxAge {
			browserTTL = manifestCacheMaxAge
		}
	}
	if sharedOK {
		age, valid := responseCacheAge(response.Header.Values("Age"))
		if !valid || age >= sharedTTL {
			sharedOK = false
			sharedTTL = 0
		} else {
			sharedTTL -= age
		}
	}
	decision := mediaCacheDecision{browserMaxAge: int(browserTTL / time.Second), immutable: directives.immutable && browserOK}
	partial := response.StatusCode == http.StatusPartialContent || response.Header.Get("Content-Range") != "" || incoming.Header.Get("Range") != ""
	if sharedOK && !partial && cacheVaryIsSafe(response.Header.Values("Vary")) {
		decision.sharedMaxAge = int(sharedTTL / time.Second)
	}
	return decision
}

func hasTargetCookie(request *http.Request, token string) bool {
	prefix := cookiePrefix(token)
	for _, cookie := range request.Cookies() {
		if strings.HasPrefix(cookie.Name, prefix) {
			return true
		}
	}
	return false
}

func requestBypassesCache(headers http.Header) bool {
	directives := parseCacheControl(headers.Values("Cache-Control"))
	if directives.invalid || directives.noCache || directives.noStore || directives.maxAge != nil && *directives.maxAge <= 0 {
		return true
	}
	return strings.Contains(strings.ToLower(strings.Join(headers.Values("Pragma"), ",")), "no-cache")
}

func parseCacheControl(values []string) cacheControlDirectives {
	var result cacheControlDirectives
	for _, value := range values {
		for _, rawDirective := range strings.Split(value, ",") {
			name, rawValue, found := strings.Cut(strings.TrimSpace(rawDirective), "=")
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "public":
				result.public = true
			case "private":
				result.private = true
			case "no-store":
				result.noStore = true
			case "no-cache":
				result.noCache = true
			case "immutable":
				result.immutable = true
			case "max-age":
				if found {
					if result.maxAge != nil {
						result.invalid = true
					} else {
						result.maxAge = cacheSeconds(rawValue)
					}
				}
			case "s-maxage":
				if found {
					if result.sharedAge != nil {
						result.invalid = true
					} else {
						result.sharedAge = cacheSeconds(rawValue)
					}
				}
			}
		}
	}
	return result
}

func cacheSeconds(raw string) *int64 {
	value, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(raw), `"`), 10, 64)
	if err != nil {
		value = -1
	}
	return &value
}

func sharedCacheTTL(directives cacheControlDirectives, cap time.Duration) (time.Duration, bool) {
	var seconds int64
	switch {
	case directives.sharedAge != nil:
		seconds = *directives.sharedAge
	case directives.maxAge != nil:
		seconds = *directives.maxAge
	case directives.public:
		seconds = 60
	default:
		return 0, false
	}
	return cappedCacheTTL(seconds, cap)
}

func browserCacheTTL(directives cacheControlDirectives, cap time.Duration) (time.Duration, bool) {
	if directives.maxAge != nil {
		return cappedCacheTTL(*directives.maxAge, cap)
	}
	if directives.public {
		return cappedCacheTTL(60, cap)
	}
	return 0, false
}

func cappedCacheTTL(seconds int64, cap time.Duration) (time.Duration, bool) {
	if seconds <= 0 {
		return 0, false
	}
	var ttl time.Duration
	if seconds > int64((24*time.Hour)/time.Second) {
		ttl = cap
	} else {
		ttl = time.Duration(seconds) * time.Second
		if ttl > cap {
			ttl = cap
		}
	}
	return ttl, ttl >= time.Second
}

func cacheVaryIsSafe(values []string) bool {
	for _, value := range values {
		for _, rawName := range strings.Split(value, ",") {
			name := strings.ToLower(strings.TrimSpace(rawName))
			if name != "" && name != "accept-encoding" {
				return false
			}
		}
	}
	return true
}

func responseCacheAge(values []string) (time.Duration, bool) {
	if len(values) == 0 {
		return 0, true
	}
	if len(values) != 1 {
		return 0, false
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(values[0]), 10, 64)
	if err != nil || seconds < 0 || seconds > int64((24*time.Hour)/time.Second) {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func isMediaOrStaticContent(contentType, path string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "video/") || strings.HasPrefix(mediaType, "audio/") || strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(mediaType, "font/") {
		return true
	}
	switch mediaType {
	case "text/css", "application/javascript", "application/x-javascript", "text/javascript", "application/wasm",
		"application/font-woff", "application/vnd.ms-fontobject", "application/vnd.apple.mpegurl",
		"application/x-mpegurl", "application/dash+xml":
		return true
	case "application/octet-stream", "binary/octet-stream":
		return hasMediaExtension(path)
	default:
		return false
	}
}

func hasMediaExtension(path string) bool {
	lower := strings.ToLower(path)
	for _, extension := range []string{".mp4", ".m4s", ".flv", ".webm", ".ts", ".m3u8", ".mpd", ".aac", ".mp3", ".m4a", ".ogg", ".wav", ".jpg", ".jpeg", ".png", ".webp", ".avif", ".gif", ".svg", ".woff", ".woff2", ".ttf", ".otf", ".js", ".wasm"} {
		if strings.HasSuffix(lower, extension) {
			return true
		}
	}
	return false
}

func isMediaManifest(contentType, path string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "application/vnd.apple.mpegurl" || mediaType == "application/x-mpegurl" || mediaType == "application/dash+xml" {
		return true
	}
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".m3u8") || strings.HasSuffix(lower, ".mpd")
}

func isHLSManifestContent(contentType, path string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch mediaType {
	case "application/vnd.apple.mpegurl", "application/x-mpegurl", "application/mpegurl", "audio/mpegurl", "audio/x-mpegurl":
		return true
	case "", "application/octet-stream", "binary/octet-stream", "text/plain":
		return strings.HasSuffix(strings.ToLower(path), ".m3u8")
	default:
		return false
	}
}

func isDASHManifestContent(contentType, path string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "application/dash+xml" {
		return true
	}
	if mediaType == "" || mediaType == "application/octet-stream" || mediaType == "binary/octet-stream" || mediaType == "text/plain" || mediaType == "application/xml" || mediaType == "text/xml" {
		return strings.HasSuffix(strings.ToLower(path), ".mpd")
	}
	return false
}

func isStreamMediaContent(contentType, path string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(mediaType, "video/") || strings.HasPrefix(mediaType, "audio/") {
		return true
	}
	if mediaType != "application/octet-stream" && mediaType != "binary/octet-stream" {
		return false
	}
	lower := strings.ToLower(path)
	for _, extension := range []string{".mp4", ".m4s", ".flv", ".webm", ".ts", ".aac", ".mp3", ".m4a", ".ogg", ".wav"} {
		if strings.HasSuffix(lower, extension) {
			return true
		}
	}
	return false
}

func rewriteResponseHeaders(headers http.Header, response *http.Response, target *url.URL, token string, secure bool, cache mediaCacheDecision) {
	headers.Del("X-OWU-Cache")
	headers.Del("X-Accel-Expires")
	if cache.browserMaxAge > 0 {
		value := "private, max-age=" + strconv.Itoa(cache.browserMaxAge)
		if cache.immutable {
			value += ", immutable"
		}
		headers.Set("Cache-Control", value)
	} else {
		headers.Set("Cache-Control", "private, no-store")
	}
	if cache.sharedMaxAge > 0 {
		// Nginx treats this marker as the only permission to store a response.
		// The target cannot forge it because copyResponseHeaders strips both it
		// and every X-Accel-* header before this policy decision runs.
		headers.Set("X-OWU-Cache", "public-media")
		headers.Set("X-Accel-Expires", strconv.Itoa(cache.sharedMaxAge))
	}
	// same-origin exposes only the OWU route to OWU itself. It is required for
	// the canonical fallback of root-relative module and runtime requests; the
	// outbound proxy rewrites the value to the target origin before forwarding.
	headers.Set("Referrer-Policy", "same-origin")
	if location := response.Header.Get("Location"); location != "" {
		if resolved, err := target.Parse(location); err == nil && (resolved.Scheme == "http" || resolved.Scheme == "https") {
			headers.Set("Location", proxyURL(resolved))
		} else {
			headers.Del("Location")
		}
	}
	if refresh := response.Header.Get("Refresh"); refresh != "" {
		headers.Set("Refresh", rewriteRefresh(refresh, target))
	}
	prefix := cookiePrefix(token)
	for _, cookie := range response.Cookies() {
		cookie.Name = prefix + cookie.Name
		cookie.Domain = ""
		cookie.Path = "/"
		cookie.Secure = secure
		if cookie.SameSite == http.SameSiteNoneMode && !secure {
			cookie.SameSite = http.SameSiteLaxMode
		}
		headers.Add("Set-Cookie", cookie.String())
	}
}

func rewriteRefresh(value string, base *url.URL) string {
	parts := strings.SplitN(value, ";", 2)
	if len(parts) != 2 {
		return value
	}
	right := strings.TrimSpace(parts[1])
	if key, raw, found := strings.Cut(right, "="); found && strings.EqualFold(strings.TrimSpace(key), "url") {
		raw = strings.Trim(strings.TrimSpace(raw), "\"'")
		if resolved, err := base.Parse(raw); err == nil && (resolved.Scheme == "http" || resolved.Scheme == "https") {
			return strings.TrimSpace(parts[0]) + "; url=" + proxyURL(resolved)
		}
	}
	return value
}
