package urlpolicy

import "testing"

func TestParseNormalizes(t *testing.T) {
	tests := []struct {
		input  string
		url    string
		origin string
		port   int
	}{
		{"https://EXAMPLE.com.:443/a/../b", "https://example.com/b", "https://example.com", 443},
		{"https://example.com:8443/", "https://example.com:8443/", "https://example.com:8443", 8443},
		{"http://example.com/a/?q=1#part", "http://example.com/a/?q=1#part", "http://example.com", 80},
		{"https://b\u00fccher.example/", "https://xn--bcher-kva.example/", "https://xn--bcher-kva.example", 443},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Parse(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.URL != tt.url || got.Origin != tt.origin || got.Port != tt.port {
				t.Fatalf("got URL=%q origin=%q port=%d", got.URL, got.Origin, got.Port)
			}
		})
	}
}

func TestParseRejectsDangerousForms(t *testing.T) {
	tests := []struct {
		input string
		code  Code
	}{
		{"example.com/docs", InvalidURL},
		{"https://user:pass@example.com/", CredentialsNotAllowed},
		{"file:///etc/passwd", SchemeNotAllowed},
		{"http://127.0.0.1/", IPLiteralNotAllowed},
		{"http://[::1]/", IPLiteralNotAllowed},
		{"https://example.com:0/", PortNotAllowed},
		{"https://example.com:/", PortNotAllowed},
		{"https://example.com:65536/", PortNotAllowed},
		{"https://example.com/%2fadmin", InvalidURL},
		{"https://example.com/?bad=%zz", InvalidURL},
		{"https://example.com\\@evil.example/", InvalidURL},
		{"https://-bad.example/", InvalidURL},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := Parse(tt.input)
			if err == nil || ErrorCode(err) != tt.code {
				t.Fatalf("got error %v (%s), want %s", err, ErrorCode(err), tt.code)
			}
		})
	}
}

func FuzzParseNeverPanics(f *testing.F) {
	for _, seed := range []string{
		"https://example.com/", "http://127.0.0.1/", "https://user:pass@example.com/", "%", "https://example.com:8443/a",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) { _, _ = Parse(input) })
}
