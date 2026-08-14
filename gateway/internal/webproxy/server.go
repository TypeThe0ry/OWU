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
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"permit-gateway/internal/safety"

	"golang.org/x/net/html/charset"
)

const (
	browsePrefix        = "/browse/"
	socketPrefix        = "/socket/"
	tunnelPrefix        = "/tunnel/"
	virtualOriginParam  = "__owu_origin_v1"
	maxRequestBodyBytes = 64 << 20
	maxRewriteBodyBytes = 16 << 20
)

type Config struct {
	ListenAddr        string
	DemoAllowedOrigin string
	TunnelKey         string
	TunnelResources   map[string]TCPResource
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
		ListenAddr:      listen,
		TunnelKey:       strings.TrimSpace(os.Getenv("OWU_TUNNEL_KEY")),
		TunnelResources: resources,
	}
}

type Server struct {
	safety            safety.Policy
	demoAllowedOrigin string
	tunnelKey         string
	tunnelResources   map[string]TCPResource
}

func New(config Config) *Server {
	return &Server{
		safety:            safePolicy(config.DemoAllowedOrigin),
		demoAllowedOrigin: config.DemoAllowedOrigin,
		tunnelKey:         config.TunnelKey,
		tunnelResources:   config.TunnelResources,
	}
}

func safePolicy(demoOrigin string) safety.Policy {
	return safety.Policy{DemoMode: demoOrigin != "", DemoAllowedOrigin: demoOrigin}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	defer transport.CloseIdleConnections()
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

	if err := writeProxyResponse(w, r, response, target, token); err != nil {
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
	host, port := target.Hostname(), targetPort(target)
	return &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		IdleConnTimeout:       30 * time.Second,
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

func writeProxyResponse(w http.ResponseWriter, incoming *http.Request, response *http.Response, target *url.URL, token string) error {
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	partialResponse := response.StatusCode == http.StatusPartialContent || response.Header.Get("Content-Range") != ""
	rewriteHTMLBody := !partialResponse && (strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml+xml"))
	rewriteCSSBody := !partialResponse && strings.Contains(contentType, "text/css")

	copyResponseHeaders(w.Header(), response.Header)
	rewriteResponseHeaders(w.Header(), response, target, token, incoming.Header.Get("X-Forwarded-Proto") == "https")

	if incoming.Method == http.MethodHead || response.Body == nil {
		w.WriteHeader(response.StatusCode)
		return nil
	}
	if rewriteHTMLBody || rewriteCSSBody {
		body, err := io.ReadAll(io.LimitReader(response.Body, maxRewriteBodyBytes+1))
		if err != nil || len(body) > maxRewriteBodyBytes {
			return errors.New("rewritable response body is too large")
		}
		if rewriteHTMLBody {
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
		} else {
			body = rewriteCSS(body, target)
		}
		w.Header().Del("Content-Encoding")
		w.Header().Del("ETag")
		w.Header().Del("Content-MD5")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(response.StatusCode)
		_, err = w.Write(body)
		return err
	}

	w.Header().Del("Content-Length")
	w.WriteHeader(response.StatusCode)
	_, err := io.Copy(w, response.Body)
	return err
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
		"service-worker-allowed": true,
		"www-authenticate":       true, "access-control-allow-origin": true, "access-control-allow-credentials": true,
	}
	for name, values := range source {
		if blocked[strings.ToLower(name)] {
			continue
		}
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func rewriteResponseHeaders(headers http.Header, response *http.Response, target *url.URL, token string, secure bool) {
	headers.Set("Cache-Control", "private, no-store")
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
