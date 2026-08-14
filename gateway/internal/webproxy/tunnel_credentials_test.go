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

// Keep the tunnel secret in the dedicated handshake header. In particular,
// fallback clients must never move it into a URL where proxy logs, browser
// history, or error messages can retain it.
func TestTunnelCredentialIsHeaderOnlyAndNeverReflected(t *testing.T) {
	server := httptest.NewServer(New(Config{
		TunnelKey: testTunnelKey,
		TunnelResources: map[string]TCPResource{
			"ssh": {ID: "ssh", Host: "127.0.0.1", Port: 1},
		},
	}))
	defer server.Close()

	tests := []struct {
		name      string
		urlSuffix string
		header    http.Header
	}{
		{
			name:      "query parameter",
			urlSuffix: "?tunnel_key=" + url.QueryEscape(testTunnelKey),
		},
		{
			name: "bearer authorization",
			header: http.Header{
				"Authorization": []string{"Bearer " + testTunnelKey},
			},
		},
		{
			name: "basic authorization",
			header: http.Header{
				"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte("owu:"+testTunnelKey))},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, server.URL+"/tunnel/ssh"+test.urlSuffix, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header = test.header.Clone()
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", response.StatusCode)
			}
			if strings.Contains(string(body), testTunnelKey) {
				t.Fatal("tunnel credential was reflected in the response")
			}
		})
	}
}
