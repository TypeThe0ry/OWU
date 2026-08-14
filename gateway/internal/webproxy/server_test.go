package webproxy

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProxyRewritesHTMLHeadersAndCookies(t *testing.T) {
	var receivedAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/private", HttpOnly: true})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><style>.x{background:url('/bg.png')}</style></head><body><a href="/next?q=1">Next</a><img src="asset.png"><form action="/search"></form></body></html>`)
	}))
	defer upstream.Close()

	origin, _ := url.Parse(upstream.URL)
	token := encodeOrigin(origin)
	server := New(Config{DemoAllowedOrigin: upstream.URL})
	request := httptest.NewRequest(http.MethodGet, browsePrefix+token+"/dir/page", nil)
	request.Header.Set("Authorization", "Basic must-not-leak")
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if receivedAuthorization != "" {
		t.Fatalf("authorization leaked upstream: %q", receivedAuthorization)
	}
	if policy := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "connect-src 'self'") {
		t.Fatal("OWU proxy CSP was not installed")
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		browsePrefix + token + "/next?q=1",
		browsePrefix + token + "/dir/asset.png",
		browsePrefix + token + "/bg.png",
		"data-owu=\"bootstrap\"",
		"MutationObserver",
		"navigator.sendBeacon",
		"window.Worker",
		"patchURLProperty",
		"CSSStyleSheet.prototype.insertRule",
		"targetFromProxyURL(proxyCandidate.href)",
		"__owu_origin_v1",
		"history.replaceState(history.state,\"\",virtualize(initialProxyTarget.href))",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("rewritten body missing %q: %s", expected, body)
		}
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !strings.HasPrefix(cookies[0].Name, cookiePrefix(token)) || !cookies[0].Secure || cookies[0].Path != "/" {
		t.Fatalf("unexpected isolated cookie: %#v", cookies)
	}
}

func TestVirtualDocumentRoutePreservesRawTargetQueryAndReferer(t *testing.T) {
	type observation struct {
		requestURI string
		origin     string
		referer    string
	}
	observed := make(chan observation, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- observation{requestURI: r.RequestURI, origin: r.Header.Get("Origin"), referer: r.Referer()}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head></head><body>virtual</body></html>`)
	}))
	defer upstream.Close()

	origin, _ := url.Parse(upstream.URL)
	token := encodeOrigin(origin)
	server := New(Config{DemoAllowedOrigin: upstream.URL})
	rawQuery := "x=a%2Bb&" + virtualOriginParam + "=legit&x=&" + virtualOriginParam + "=" + token
	request := httptest.NewRequest(http.MethodGet, "/en/g/game?"+rawQuery, nil)
	request.Header.Set("Origin", "https://owu.example")
	request.Header.Set("Referer", "https://owu.example/en/g/source?from=a%2Bb&"+virtualOriginParam+"="+token)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	got := <-observed
	wantURI := "/en/g/game?x=a%2Bb&" + virtualOriginParam + "=legit&x="
	if got.requestURI != wantURI {
		t.Fatalf("upstream RequestURI = %q, want %q", got.requestURI, wantURI)
	}
	if got.origin != upstream.URL || got.referer != upstream.URL+"/en/g/source?from=a%2Bb" {
		t.Fatalf("upstream source headers = origin %q, referer %q", got.origin, got.referer)
	}
}

func TestVirtualRefererFallbackReturnsCanonicalBrowseRoute(t *testing.T) {
	server := New(Config{})
	target, _ := url.Parse("https://example.com/")
	token := encodeOrigin(target)
	request := httptest.NewRequest(http.MethodGet, "/runtime.js?v=1", nil)
	request.Header.Set("Referer", "https://owu.example/app?x=1&"+virtualOriginParam+"="+token)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got, want := recorder.Header().Get("Location"), browsePrefix+token+"/runtime.js?v=1"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestVirtualRouteWinsInternalLookingPathAndKeepsDoubleSlash(t *testing.T) {
	requestPaths := make(chan string, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPaths <- r.RequestURI
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "upstream")
	}))
	defer upstream.Close()

	origin, _ := url.Parse(upstream.URL)
	token := encodeOrigin(origin)
	server := New(Config{DemoAllowedOrigin: upstream.URL})
	for _, path := range []string{"/healthz", "//cdn.example/path"} {
		request := httptest.NewRequest(http.MethodGet, path+"?"+virtualOriginParam+"="+token, nil)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || recorder.Body.String() != "upstream" {
			t.Fatalf("%s status = %d, body = %q", path, recorder.Code, recorder.Body.String())
		}
		if got := <-requestPaths; got != path {
			t.Fatalf("upstream RequestURI = %q, want %q", got, path)
		}
	}
}

func TestProxyRewritesCrossOriginRedirect(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://example.com/destination?x=1")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	origin, _ := url.Parse(upstream.URL)
	server := New(Config{DemoAllowedOrigin: upstream.URL})
	request := httptest.NewRequest(http.MethodGet, browsePrefix+encodeOrigin(origin)+"/start", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	exampleToken := base64.RawURLEncoding.EncodeToString([]byte("https://example.com"))
	if got, want := recorder.Header().Get("Location"), browsePrefix+exampleToken+"/destination?x=1"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestProxyBlocksLoopbackWithoutTestException(t *testing.T) {
	target, _ := url.Parse("http://127.0.0.1:6553/")
	server := New(Config{})
	request := httptest.NewRequest(http.MethodGet, browsePrefix+encodeOrigin(target)+"/", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestRefererFallbackRedirectsToCanonicalBrowseRoute(t *testing.T) {
	server := New(Config{})
	target, _ := url.Parse("https://example.com/app/page")
	request := httptest.NewRequest(http.MethodGet, "/runtime-chunk.js?v=1", nil)
	request.Header.Set("Referer", "https://owu.example"+proxyURL(target))
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", recorder.Code)
	}
	want := proxyURL(mustURL(t, "https://example.com/runtime-chunk.js?v=1"))
	if got := recorder.Header().Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestProxyOmitsGetBodyFramingAndPreservesEntityBodies(t *testing.T) {
	type observation struct {
		method           string
		contentLength    int64
		transferEncoding []string
		body             string
	}
	observations := make(chan observation, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		observations <- observation{
			method:           r.Method,
			contentLength:    r.ContentLength,
			transferEncoding: append([]string(nil), r.TransferEncoding...),
			body:             string(body),
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	origin, _ := url.Parse(upstream.URL)
	server := New(Config{DemoAllowedOrigin: upstream.URL})
	for _, test := range []struct {
		method string
		body   string
	}{
		{method: http.MethodGet},
		{method: http.MethodGet, body: "get-payload"},
		{method: http.MethodPost, body: "post-payload"},
		{method: http.MethodPatch},
	} {
		var body io.Reader
		if test.body != "" {
			body = strings.NewReader(test.body)
		}
		request := httptest.NewRequest(test.method, browsePrefix+encodeOrigin(origin)+"/echo", body)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", test.method, recorder.Code, recorder.Body.String())
		}
	}

	get := <-observations
	if get.method != http.MethodGet || get.contentLength != 0 || len(get.transferEncoding) != 0 || get.body != "" {
		t.Fatalf("GET unexpectedly carried request-body framing: %#v", get)
	}
	getWithBody := <-observations
	if getWithBody.method != http.MethodGet || getWithBody.contentLength != int64(len("get-payload")) || len(getWithBody.transferEncoding) != 0 || getWithBody.body != "get-payload" {
		t.Fatalf("non-empty GET body semantics were not preserved: %#v", getWithBody)
	}
	post := <-observations
	if post.method != http.MethodPost || post.contentLength != int64(len("post-payload")) || len(post.transferEncoding) != 0 || post.body != "post-payload" {
		t.Fatalf("POST body semantics were not preserved: %#v", post)
	}
	patch := <-observations
	if patch.method != http.MethodPatch || patch.contentLength != 0 || len(patch.transferEncoding) != 0 || patch.body != "" {
		t.Fatalf("empty PATCH semantics were not preserved: %#v", patch)
	}
}

func TestProxyPeeksUnknownLengthGetAndHeadBodies(t *testing.T) {
	type observation struct {
		method           string
		contentLength    int64
		transferEncoding []string
		body             string
	}
	observations := make(chan observation, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		observations <- observation{
			method:           r.Method,
			contentLength:    r.ContentLength,
			transferEncoding: append([]string(nil), r.TransferEncoding...),
			body:             string(body),
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	origin, _ := url.Parse(upstream.URL)
	server := New(Config{DemoAllowedOrigin: upstream.URL})
	tests := []struct {
		name   string
		method string
		body   string
	}{
		{name: "empty chunked GET", method: http.MethodGet},
		{name: "non-empty chunked GET", method: http.MethodGet, body: "chunked-get"},
		{name: "empty chunked HEAD", method: http.MethodHead},
		{name: "non-empty chunked HEAD", method: http.MethodHead, body: "chunked-head"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, browsePrefix+encodeOrigin(origin)+"/stream", nil)
		request.Body = io.NopCloser(strings.NewReader(test.body))
		request.ContentLength = -1
		request.TransferEncoding = []string{"chunked"}
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", test.name, recorder.Code, recorder.Body.String())
		}
	}

	for _, test := range tests {
		got := <-observations
		if got.method != test.method || got.body != test.body {
			t.Fatalf("%s body semantics were not preserved: %#v", test.name, got)
		}
		if test.body == "" {
			if got.contentLength != 0 || len(got.transferEncoding) != 0 {
				t.Fatalf("%s unexpectedly carried request-body framing: %#v", test.name, got)
			}
		} else if got.contentLength != -1 || len(got.transferEncoding) != 1 || got.transferEncoding[0] != "chunked" {
			t.Fatalf("%s lost chunked request-body framing: %#v", test.name, got)
		}
	}
}
