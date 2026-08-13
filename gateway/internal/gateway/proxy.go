package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBodyBytes int64 = 64 << 20

var errResponseTooLarge = errors.New("upstream response exceeded the configured limit")

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	started, reqID := time.Now(), requestID()
	if allowed, retry := s.dataLimiter.allow("data:"+clientPrefix(r.RemoteAddr), started); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many attempts. Wait a moment, then try again.", reqID)
		return
	}
	if strings.HasPrefix(r.RequestURI, "http://") || strings.HasPrefix(r.RequestURI, "https://") || r.Method == http.MethodConnect || r.Method == http.MethodTrace {
		writeError(w, http.StatusBadRequest, "REQUEST_FORM_NOT_ALLOWED", "This request form is not supported.", reqID)
		return
	}
	state, resource, err := s.actorSession(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Open an approved resource to start a session.", reqID)
		return
	}
	if !resource.AllowedMethods[r.Method] {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "This method is not approved for the resource.", reqID)
		s.auditDecision(started, reqID, state.ActorID, resource, "http.request", r, "deny", "method_not_allowed", http.StatusMethodNotAllowed)
		return
	}
	if !resource.AllowsPath(r.URL.Path) {
		writeError(w, http.StatusForbidden, "PATH_NOT_ALLOWED", "This path is not approved for the resource.", reqID)
		s.auditDecision(started, reqID, state.ActorID, resource, "http.request", r, "deny", "path_not_allowed", http.StatusForbidden)
		return
	}
	websocket := isWebSocket(r)
	if websocket && !resource.WebSocketEnabled {
		writeError(w, http.StatusForbidden, "WEBSOCKET_NOT_ALLOWED", "WebSocket access is not approved for this resource.", reqID)
		return
	}
	requestLifetime := 2 * time.Minute
	if websocket {
		requestLifetime = time.Hour
	}
	requestContext, cancelRequest := context.WithTimeout(r.Context(), requestLifetime)
	defer cancelRequest()
	r = r.WithContext(requestContext)
	if r.ContentLength > maxRequestBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "The request body is too large.", reqID)
		return
	}
	release, ok := s.acquire(resource.ID)
	if !ok {
		writeError(w, http.StatusTooManyRequests, "CONCURRENCY_LIMITED", "This resource has too many active connections.", reqID)
		return
	}
	defer release()
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	target, _ := url.Parse(resource.Origin.Origin)
	transport := s.transportFor(resource)
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(proxyRequest *httputil.ProxyRequest) {
			proxyRequest.SetURL(target)
			proxyRequest.Out.Host = target.Host
			sanitizeOutbound(proxyRequest.Out, s.sessionCookieName())
			s.rewriteBrowserOrigin(proxyRequest.Out, resource)
			if websocket {
				proxyRequest.Out.Header.Set("Connection", "Upgrade")
				proxyRequest.Out.Header.Set("Upgrade", "websocket")
				proxyRequest.Out.Header.Del("Sec-WebSocket-Extensions")
			}
		},
		ModifyResponse: func(response *http.Response) error {
			return s.modifyResponse(response, resource)
		},
		ErrorHandler: func(writer http.ResponseWriter, request *http.Request, proxyErr error) {
			writeError(writer, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", "The resource didn't respond. Check the address or try again later.", reqID)
		},
		FlushInterval: 100 * time.Millisecond,
	}
	wrapped := &auditResponseWriter{ResponseWriter: w}
	proxy.ServeHTTP(wrapped, r)
	status := wrapped.status
	if status == 0 {
		status = http.StatusOK
	}
	action := "http.request"
	if websocket {
		action = "websocket.connect"
	}
	s.audit.write(auditEvent{OccurredAt: time.Now().UTC(), RequestID: reqID, ActorID: state.ActorID, ResourceID: resource.ID, Action: action, Method: r.Method, PathHash: pathHash(r.URL.Path), Decision: "allow", Reason: "public_grant_active", StatusCode: status, BytesIn: max(0, r.ContentLength), BytesOut: wrapped.bytes, DurationMS: time.Since(started).Milliseconds(), ClientNet: clientPrefix(r.RemoteAddr)})
}

func (s *Server) transportFor(resource Resource) *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		DisableKeepAlives:     true,
		MaxIdleConns:          0,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: resource.Origin.Host,
		},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return s.dialAuthorized(ctx, network, address, resource)
		},
	}
}

func (s *Server) dialAuthorized(ctx context.Context, network, requestedAddress string, resource Resource) (net.Conn, error) {
	host, port, err := net.SplitHostPort(requestedAddress)
	if err != nil || !strings.EqualFold(strings.TrimSuffix(host, "."), resource.Origin.Host) || port != strconv.Itoa(resource.Origin.Port) {
		return nil, errors.New("upstream dial target does not match the authorized resource")
	}
	addresses, err := s.safety.Resolve(ctx, resource.Origin.Scheme, resource.Origin.Host, resource.Origin.Port)
	if err != nil {
		return nil, err
	}
	var failures []error
	for _, address := range addresses {
		pinned := net.JoinHostPort(address.String(), port)
		var connection net.Conn
		if s.connect != nil {
			connection, err = s.connect(ctx, network, pinned)
		} else {
			dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
			connection, err = dialer.DialContext(ctx, network, pinned)
		}
		if err == nil {
			return connection, nil
		}
		failures = append(failures, err)
	}
	return nil, fmt.Errorf("connect to authorized target: %w", errors.Join(failures...))
}

func sanitizeOutbound(request *http.Request, sessionCookie string) {
	removeConnectionHeaders(request.Header)
	for name := range request.Header {
		lower := strings.ToLower(name)
		if lower == "forwarded" || strings.HasPrefix(lower, "x-forwarded-") || strings.HasPrefix(lower, "proxy-") {
			request.Header.Del(name)
		}
	}
	cookies := request.Cookies()
	request.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name != sessionCookie {
			request.AddCookie(cookie)
		}
	}
}

func removeConnectionHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		// Upgrade is restored by ReverseProxy for a valid WebSocket request.
		header.Del(name)
	}
}

func (s *Server) rewriteBrowserOrigin(request *http.Request, resource Resource) {
	base, _ := url.Parse(s.config.PublicBaseURL)
	upstream, _ := url.Parse(resource.Origin.Origin)
	if raw := request.Header.Get("Origin"); raw != "" {
		if origin, err := url.Parse(raw); err == nil && sameURLOrigin(origin, base) {
			request.Header.Set("Origin", upstream.Scheme+"://"+upstream.Host)
		}
	}
	if raw := request.Header.Get("Referer"); raw != "" {
		if referer, err := url.Parse(raw); err == nil && sameURLOrigin(referer, base) {
			referer.Scheme, referer.Host = upstream.Scheme, upstream.Host
			request.Header.Set("Referer", referer.String())
		}
	}
}

func sameURLOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func (s *Server) modifyResponse(response *http.Response, resource Resource) error {
	if response.StatusCode != http.StatusSwitchingProtocols {
		removeConnectionHeaders(response.Header)
	}
	response.Header.Set("Cache-Control", "private, no-store")
	response.Header.Set("X-Content-Type-Options", "nosniff")
	if location := response.Header.Get("Location"); location != "" {
		parsed, err := response.Request.URL.Parse(location)
		if err != nil {
			return errors.New("upstream returned an invalid redirect")
		}
		if parsed.IsAbs() {
			same := strings.EqualFold(parsed.Scheme, resource.Origin.Scheme) && strings.EqualFold(parsed.Hostname(), resource.Origin.Host)
			port := parsed.Port()
			if port == "" {
				if parsed.Scheme == "https" {
					port = "443"
				} else {
					port = "80"
				}
			}
			same = same && port == strconv.Itoa(resource.Origin.Port)
			if !same {
				_ = response.Body.Close()
				body := `{"error":{"code":"CROSS_ORIGIN_REDIRECT","message":"The resource tried to redirect to a different, unapproved origin."}}` + "\n"
				response.StatusCode = http.StatusConflict
				response.Status = "409 Conflict"
				response.Body = io.NopCloser(strings.NewReader(body))
				response.ContentLength = int64(len(body))
				response.Header.Del("Location")
				response.Header.Set("Content-Type", "application/json")
				response.Header.Set("Content-Length", strconv.Itoa(len(body)))
			} else {
				mapped := &url.URL{Path: parsed.Path, RawPath: parsed.RawPath, RawQuery: parsed.RawQuery, Fragment: parsed.Fragment}
				response.Header.Set("Location", s.config.PublicBaseURL+mapped.String())
			}
		}
	}
	sanitizeResponseCookies(response, resource.Origin.Host)
	if response.StatusCode != http.StatusSwitchingProtocols && response.Body != nil {
		response.Body = &limitedBody{ReadCloser: response.Body, remaining: maxResponseBodyBytes}
	}
	return nil
}

func sanitizeResponseCookies(response *http.Response, upstreamHost string) {
	cookies := response.Cookies()
	response.Header.Del("Set-Cookie")
	for _, cookie := range cookies {
		if strings.HasPrefix(cookie.Name, "__Host-aa_") || strings.HasPrefix(cookie.Name, "__aa_") {
			continue
		}
		if cookie.Domain != "" {
			domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
			if domain != strings.ToLower(upstreamHost) {
				continue
			}
			cookie.Domain = ""
		}
		response.Header.Add("Set-Cookie", cookie.String())
	}
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && headerHasToken(r.Header, "Connection", "upgrade")
}

func headerHasToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

type limitedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *limitedBody) Read(destination []byte) (int, error) {
	if b.remaining == 0 {
		var one [1]byte
		n, err := b.ReadCloser.Read(one[:])
		if n > 0 {
			return 0, errResponseTooLarge
		}
		return 0, err
	}
	if int64(len(destination)) > b.remaining {
		destination = destination[:b.remaining]
	}
	n, err := b.ReadCloser.Read(destination)
	b.remaining -= int64(n)
	return n, err
}

type auditResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *auditResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditResponseWriter) Write(value []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(value)
	w.bytes += int64(n)
	return n, err
}

func (w *auditResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *auditResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *auditResponseWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func (w *auditResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
