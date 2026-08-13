package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"permit-gateway/internal/urlpolicy"
)

type testResolver struct {
	mu        sync.Mutex
	addresses map[string][]netip.Addr
	calls     int
	sequence  [][]netip.Addr
}

func (r *testResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if len(r.sequence) > 0 {
		index := r.calls - 1
		if index >= len(r.sequence) {
			index = len(r.sequence) - 1
		}
		return r.sequence[index], nil
	}
	addresses, ok := r.addresses[host]
	if !ok {
		return nil, errors.New("host not found")
	}
	return addresses, nil
}

func TestPublicLaunchAndProxy(t *testing.T) {
	var upstreamHost, upstreamPath, forwarded, sessionCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHost, upstreamPath = r.Host, r.URL.RequestURI()
		forwarded = r.Header.Get("X-Forwarded-For")
		sessionCookie = r.Header.Get("Cookie")
		w.Header().Add("Set-Cookie", "app=value; Domain=demo-target; Path=/")
		w.Header().Add("Set-Cookie", "__Host-aa_session=stolen; Path=/")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "demo response")
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)
	_, upstreamPort, _ := net.SplitHostPort(upstreamURL.Host)
	origin := "http://demo-target:" + upstreamPort
	resource := mustResource(t, origin, true)
	config := testConfig(origin, resource)
	server, err := New(config, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	server.safety.Resolver = &testResolver{addresses: map[string][]netip.Addr{"demo-target": {netip.MustParseAddr("172.20.0.8")}}}
	server.connect = func(ctx context.Context, network, pinned string) (net.Conn, error) {
		if !strings.HasPrefix(pinned, "172.20.0.8:") {
			t.Errorf("dial was not pinned: %s", pinned)
		}
		return (&net.Dialer{}).DialContext(ctx, network, upstreamURL.Host)
	}
	gatewayServer := httptest.NewServer(server)
	defer gatewayServer.Close()

	check := postAccess(t, gatewayServer.URL, origin+"/hello?q=1#result")
	if check.Decision != "allowed" || check.LaunchURL == "" || check.ExpiresAt == nil {
		t.Fatalf("unexpected access response: %+v", check)
	}
	launchURL, _ := url.Parse(check.LaunchURL)
	if !strings.HasPrefix(launchURL.Path, "/_launch/") {
		t.Fatalf("launch path=%q", launchURL.Path)
	}
	launchRequest, _ := http.NewRequest(http.MethodGet, gatewayServer.URL+launchURL.RequestURI(), nil)
	launchResponse, err := noRedirectClient().Do(launchRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer launchResponse.Body.Close()
	if launchResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("launch status=%d", launchResponse.StatusCode)
	}
	if got := launchResponse.Header.Get("Location"); got != "/hello?q=1#result" {
		t.Fatalf("location=%q", got)
	}
	cookies := launchResponse.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "aa_demo_session" {
		t.Fatalf("session cookies=%v", cookies)
	}

	proxyRequest, _ := http.NewRequest(http.MethodGet, gatewayServer.URL+"/hello?q=1", nil)
	proxyRequest.AddCookie(cookies[0])
	proxyRequest.Header.Set("X-Forwarded-For", "192.0.2.3")
	proxyResponse, err := http.DefaultClient.Do(proxyRequest)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(proxyResponse.Body)
	_ = proxyResponse.Body.Close()
	if string(body) != "demo response" {
		t.Fatalf("body=%q", body)
	}
	if upstreamHost != "demo-target:"+upstreamPort || upstreamPath != "/hello?q=1" {
		t.Fatalf("host=%q path=%q", upstreamHost, upstreamPath)
	}
	if forwarded != "" {
		t.Fatalf("client forwarding header leaked: %q", forwarded)
	}
	if sessionCookie != "" {
		t.Fatalf("gateway session leaked upstream: %q", sessionCookie)
	}
	setCookies := proxyResponse.Cookies()
	if len(setCookies) != 1 || setCookies[0].Name != "app" || setCookies[0].Domain != "" {
		t.Fatalf("sanitized cookies=%v", setCookies)
	}

	replay, err := noRedirectClient().Get(gatewayServer.URL + launchURL.RequestURI())
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Body.Close()
	if replay.StatusCode != http.StatusGone {
		t.Fatalf("ticket replay status=%d", replay.StatusCode)
	}
}

func TestUnregisteredOriginDoesNotResolve(t *testing.T) {
	origin := "http://demo-target:9000"
	resource := mustResource(t, origin, true)
	server, _ := New(testConfig(origin, resource), io.Discard)
	resolver := &testResolver{addresses: map[string][]netip.Addr{"demo-target": {netip.MustParseAddr("172.20.0.8")}}}
	server.safety.Resolver = resolver
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	response := postAccess(t, httpServer.URL, "https://unregistered.example/path")
	if response.Decision != "resource_not_authorized" {
		t.Fatalf("decision=%s", response.Decision)
	}
	if resolver.calls != 0 {
		t.Fatalf("unregistered target caused %d DNS calls", resolver.calls)
	}
}

func TestPortMismatchDecision(t *testing.T) {
	origin := "http://demo-target:9000"
	server, _ := New(testConfig(origin, mustResource(t, origin, true)), io.Discard)
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	response := postAccess(t, httpServer.URL, "http://demo-target:9001/")
	if response.Decision != "port_not_allowed" {
		t.Fatalf("decision=%s", response.Decision)
	}
}

func TestDNSRebindingFailsClosedAtLaunch(t *testing.T) {
	origin := "http://demo-target:9000"
	server, _ := New(testConfig(origin, mustResource(t, origin, true)), io.Discard)
	server.safety.Resolver = &testResolver{sequence: [][]netip.Addr{
		{netip.MustParseAddr("172.20.0.8")},
		{netip.MustParseAddr("169.254.169.254")},
	}}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	check := postAccess(t, httpServer.URL, origin+"/")
	if check.Decision != "allowed" {
		t.Fatalf("check=%+v", check)
	}
	launchURL, _ := url.Parse(check.LaunchURL)
	response, err := noRedirectClient().Get(httpServer.URL + launchURL.RequestURI())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("launch status=%d", response.StatusCode)
	}
}

func TestDNSRebindingFailsClosedAtConnectTime(t *testing.T) {
	origin := "http://demo-target:9000"
	server, _ := New(testConfig(origin, mustResource(t, origin, true)), io.Discard)
	server.safety.Resolver = &testResolver{sequence: [][]netip.Addr{
		{netip.MustParseAddr("172.20.0.8")},
		{netip.MustParseAddr("172.20.0.8")},
		{netip.MustParseAddr("169.254.169.254")},
	}}
	connectCalls := 0
	server.connect = func(context.Context, string, string) (net.Conn, error) {
		connectCalls++
		return nil, errors.New("must not connect")
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	check := postAccess(t, httpServer.URL, origin+"/")
	launchURL, _ := url.Parse(check.LaunchURL)
	launchResponse, err := noRedirectClient().Get(httpServer.URL + launchURL.RequestURI())
	if err != nil {
		t.Fatal(err)
	}
	_ = launchResponse.Body.Close()
	cookies := launchResponse.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/", nil)
	request.AddCookie(cookies[0])
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("proxy status=%d", response.StatusCode)
	}
	if connectCalls != 0 {
		t.Fatalf("unsafe rebound address reached connector %d times", connectCalls)
	}
}

func TestPrivateResourceIsDeniedEvenWithForgedHeaders(t *testing.T) {
	origin := "http://demo-target:9000"
	resource := mustResource(t, origin, false)
	server, _ := New(testConfig(origin, resource), io.Discard)
	server.safety.Resolver = &testResolver{addresses: map[string][]netip.Addr{"demo-target": {netip.MustParseAddr("172.20.0.8")}}}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	response := postAccess(t, httpServer.URL, origin+"/")
	if response.Decision != "resource_not_authorized" {
		t.Fatalf("decision=%s", response.Decision)
	}

	body := bytes.NewBufferString(`{"input_url":"` + origin + `/"}`)
	request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/access/check", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Permit-Demo-User", "demo-user")
	result, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	var allowed accessResponse
	if err := json.NewDecoder(result.Body).Decode(&allowed); err != nil {
		t.Fatal(err)
	}
	if allowed.Decision != "resource_not_authorized" {
		t.Fatalf("decision=%s", allowed.Decision)
	}
}

func postAccess(t *testing.T, baseURL, input string) accessResponse {
	t.Helper()
	payload, _ := json.Marshal(accessRequest{InputURL: input})
	response, err := http.Post(baseURL+"/v1/access/check", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result accessResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func mustResource(t *testing.T, origin string, public bool) Resource {
	t.Helper()
	normalized, err := urlpolicy.Parse(origin)
	if err != nil {
		t.Fatal(err)
	}
	return Resource{ID: "res_demo", PublicID: "demo", DisplayName: "Demo", Origin: normalized, Public: public, AllowedPathPrefixes: []string{"/"}, AllowedMethods: map[string]bool{"GET": true, "POST": true}, WebSocketEnabled: true}
}

func testConfig(origin string, resource Resource) Config {
	return Config{PublicBaseURL: "http://gateway.test", DemoMode: true, DemoTargetOrigin: origin, SessionSecret: []byte(strings.Repeat("s", 32)), Resources: []Resource{resource}, TicketTTL: time.Minute, SessionTTL: time.Minute}
}

func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}
