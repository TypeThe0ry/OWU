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
		"text.startsWith(browsePrefix)",
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
