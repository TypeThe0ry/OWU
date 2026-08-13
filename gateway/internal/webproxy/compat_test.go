package webproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestClientBootstrapKeepsWSSAndBridgesCookies(t *testing.T) {
	target, err := url.Parse("https://target.example/app/")
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := clientBootstrap(target)
	prefixJSON, _ := json.Marshal(cookiePrefix(encodeOrigin(target)))

	for _, expected := range []string{
		`const cookiePrefix=` + string(prefixJSON) + `;`,
		`parsed.protocol==="wss:"||parsed.protocol==="https:"?"wss:":"ws:"`,
		`Object.defineProperty(cookieOwner,"cookie"`,
		`name.startsWith(cookiePrefix)`,
		`key!=="domain"&&key!=="path"`,
		`attributes.push("Path=/")`,
		`Promise.reject(new DOMException(serviceWorkerMessage,"SecurityError"))`,
		`registration.unregister()`,
		`OWU disables Service Worker registration because proxied sites share one browser origin.`,
		`window.navigation.addEventListener("navigate"`,
	} {
		if !strings.Contains(bootstrap, expected) {
			t.Fatalf("bootstrap missing %q", expected)
		}
	}
	if strings.Contains(bootstrap, `const targetScheme=parsed.protocol==="https:"?"wss:":"ws:"`) {
		t.Fatal("bootstrap still downgrades an explicit wss:// URL to ws://")
	}
}

func TestRewriteImportMapURLs(t *testing.T) {
	base, err := url.Parse("https://example.test/app/page.html")
	if err != nil {
		t.Fatal(err)
	}
	source := `{
		"imports": {
			"app": "./js/app.js",
			"cdn": "https://cdn.example/module.js",
			"bare": "react",
			"disabled": null
		},
		"scopes": {
			"/scope/": {"lib": "../lib.js"}
		},
		"integrity": {
			"./js/app.js": "sha384-test"
		}
	}`

	rewritten := rewriteImportMap(source, base)
	var document map[string]any
	if err := json.Unmarshal([]byte(rewritten), &document); err != nil {
		t.Fatalf("rewritten import map is invalid JSON: %v\n%s", err, rewritten)
	}
	imports := document["imports"].(map[string]any)
	if got, want := imports["app"], proxyURL(mustURL(t, "https://example.test/app/js/app.js")); got != want {
		t.Fatalf("relative import = %v, want %q", got, want)
	}
	if got, want := imports["cdn"], proxyURL(mustURL(t, "https://cdn.example/module.js")); got != want {
		t.Fatalf("absolute import = %v, want %q", got, want)
	}
	if got := imports["bare"]; got != "react" {
		t.Fatalf("bare import address was incorrectly rewritten: %v", got)
	}
	if got := imports["disabled"]; got != nil {
		t.Fatalf("null import address changed: %v", got)
	}

	scopes := document["scopes"].(map[string]any)
	scopeKey := proxyURL(mustURL(t, "https://example.test/scope/"))
	scopeImports, ok := scopes[scopeKey].(map[string]any)
	if !ok {
		t.Fatalf("rewritten scope %q missing: %#v", scopeKey, scopes)
	}
	if got, want := scopeImports["lib"], proxyURL(mustURL(t, "https://example.test/lib.js")); got != want {
		t.Fatalf("scoped import = %v, want %q", got, want)
	}

	integrity := document["integrity"].(map[string]any)
	resourceKey := proxyURL(mustURL(t, "https://example.test/app/js/app.js"))
	if got := integrity[resourceKey]; got != "sha384-test" {
		t.Fatalf("rewritten integrity entry = %v", got)
	}
}

func TestProxyDecodesLegacyHTMLToUTF8(t *testing.T) {
	target := mustURL(t, "https://example.test/legacy/page.html")
	body := append([]byte(`<!doctype html><html><head></head><body><a href="/next">caf`), byte(0xe9))
	body = append(body, []byte(`</a></body></html>`)...)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	response.Header.Set("Content-Type", "text/html; charset=windows-1252")
	incoming := httptest.NewRequest(http.MethodGet, "https://proxy.example"+proxyURL(target), nil)
	recorder := httptest.NewRecorder()

	if err := writeProxyResponse(recorder, incoming, response, target, encodeOrigin(target)); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	mediaType, parameters, err := mime.ParseMediaType(recorder.Header().Get("Content-Type"))
	if err != nil || mediaType != "text/html" || !strings.EqualFold(parameters["charset"], "utf-8") {
		t.Fatalf("Content-Type = %q", recorder.Header().Get("Content-Type"))
	}
	result := recorder.Body.String()
	if !strings.Contains(result, "café") || strings.Contains(result, "caf�") {
		t.Fatalf("legacy text was not decoded to UTF-8: %q", result)
	}
	if !strings.Contains(result, proxyURL(mustURL(t, "https://example.test/next"))) {
		t.Fatalf("decoded HTML was not URL-rewritten: %s", result)
	}
}

func TestPartialHTMLAndCSSPassThroughUnparsed(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		contentType  string
		contentRange string
		body         string
	}{
		{
			name:         "status 206 HTML",
			status:       http.StatusPartialContent,
			contentType:  "text/html; charset=utf-8",
			contentRange: "bytes 0-37/100",
			body:         `<a href="/must-stay-raw">chunk</a>`,
		},
		{
			name:         "Content-Range CSS",
			status:       http.StatusOK,
			contentType:  "text/css",
			contentRange: "bytes 10-42/100",
			body:         `.x{background:url('/must-stay-raw')}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := mustURL(t, "https://example.test/chunk")
			response := &http.Response{
				StatusCode: test.status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}
			response.Header.Set("Content-Type", test.contentType)
			response.Header.Set("Content-Range", test.contentRange)
			incoming := httptest.NewRequest(http.MethodGet, "https://proxy.example"+proxyURL(target), nil)
			recorder := httptest.NewRecorder()

			if err := writeProxyResponse(recorder, incoming, response, target, encodeOrigin(target)); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
			if got := recorder.Header().Get("Content-Range"); got != test.contentRange {
				t.Fatalf("Content-Range = %q, want %q", got, test.contentRange)
			}
			if got := recorder.Body.String(); got != test.body {
				t.Fatalf("partial body was rewritten:\n got %q\nwant %q", got, test.body)
			}
			if recorder.Header().Get("Content-Security-Policy") != "" {
				t.Fatal("partial HTML unexpectedly entered the HTML rewrite path")
			}
		})
	}
}

func TestProxyURLAddsSlashForOriginRoot(t *testing.T) {
	target := mustURL(t, "https://example.test")
	if got, want := proxyURL(target), browsePrefix+encodeOrigin(target)+"/"; got != want {
		t.Fatalf("proxyURL(root) = %q, want %q", got, want)
	}
}

func TestOriginTokenCanonicalizesCaseTrailingDotAndDefaultPort(t *testing.T) {
	variants := []*url.URL{
		mustURL(t, "https://EXAMPLE.test"),
		mustURL(t, "https://example.test.:443"),
		mustURL(t, "HTTPS://example.test/"),
	}
	want := encodeOrigin(mustURL(t, "https://example.test"))
	for _, variant := range variants {
		if got := encodeOrigin(variant); got != want {
			t.Fatalf("encodeOrigin(%q) = %q, want %q", variant, got, want)
		}
	}
	decoded, err := decodeOrigin(want, map[string]bool{"https": true})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Scheme != "https" || decoded.Host != "example.test" {
		t.Fatalf("decoded canonical origin = %q", decoded.String())
	}
}

func TestServiceWorkerAllowedResponseHeaderIsBlocked(t *testing.T) {
	source := make(http.Header)
	source.Set("Service-Worker-Allowed", "/")
	source.Set("Content-Type", "application/javascript")
	destination := make(http.Header)
	copyResponseHeaders(destination, source)
	if got := destination.Get("Service-Worker-Allowed"); got != "" {
		t.Fatalf("Service-Worker-Allowed leaked through proxy: %q", got)
	}
	if got := destination.Get("Content-Type"); got != "application/javascript" {
		t.Fatalf("unrelated response header was lost: %q", got)
	}
}

func TestRequestOriginAndRefererUseTheSourceProxyRoute(t *testing.T) {
	source := mustURL(t, "https://source.example/app/page?q=1")
	destination := mustURL(t, "https://api.example/write")
	incoming := httptest.NewRequest(http.MethodPost, "https://owu.example"+proxyURL(destination), nil)
	incoming.Header.Set("Origin", "https://owu.example")
	incoming.Header.Set("Referer", "https://owu.example"+proxyURL(source))
	incoming.Header.Set("Sec-Fetch-Site", "same-origin")
	outbound := httptest.NewRequest(http.MethodPost, destination.String(), nil)
	copyRequestHeaders(outbound.Header, incoming.Header)
	rewriteRequestOrigin(outbound, incoming)

	if got := outbound.Header.Get("Origin"); got != "https://source.example" {
		t.Fatalf("Origin = %q", got)
	}
	if got := outbound.Header.Get("Referer"); got != source.String() {
		t.Fatalf("Referer = %q, want %q", got, source)
	}
	if got := outbound.Header.Get("Sec-Fetch-Site"); got != "" {
		t.Fatalf("stale Sec-Fetch-Site leaked upstream: %q", got)
	}
}

func TestWebSocketProxyBridgesBinaryFrames(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer connection.CloseNow()
		messageType, payload, err := connection.Read(r.Context())
		if err == nil {
			_ = connection.Write(r.Context(), messageType, payload)
		}
	}))
	defer upstream.Close()

	upstreamOrigin := mustURL(t, upstream.URL)
	websocketOrigin := *upstreamOrigin
	websocketOrigin.Scheme = "ws"
	proxy := httptest.NewServer(New(Config{DemoAllowedOrigin: upstream.URL}))
	defer proxy.Close()

	endpoint := strings.Replace(proxy.URL, "http://", "ws://", 1) + socketPrefix + encodeOrigin(&websocketOrigin) + "/echo"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, response, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		if response != nil {
			t.Fatalf("WebSocket proxy status %d: %v", response.StatusCode, err)
		}
		t.Fatal(err)
	}
	defer connection.CloseNow()

	payload := []byte{0, 1, 2, 0xff, 'O', 'W', 'U'}
	if err := connection.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatal(err)
	}
	messageType, result, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if messageType != websocket.MessageBinary || !bytes.Equal(result, payload) {
		t.Fatalf("WebSocket echo = type %v payload %v", messageType, result)
	}
}

func TestHTMLImportMapIsRewritten(t *testing.T) {
	base := mustURL(t, "https://example.test/app/page.html")
	body := []byte(`<!doctype html><html><head><script type="importmap">{"imports":{"app":"./app.js"}}</script></head><body></body></html>`)
	rewritten, err := rewriteHTML(body, base)
	if err != nil {
		t.Fatal(err)
	}
	expected := `"app":"` + proxyURL(mustURL(t, "https://example.test/app/app.js")) + `"`
	if !strings.Contains(string(rewritten), expected) {
		t.Fatalf("rewritten HTML import map missing %q:\n%s", expected, rewritten)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
