package safety

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

type staticResolver map[string][]netip.Addr

func (r staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	addresses, ok := r[host]
	if !ok {
		return nil, errors.New("not found")
	}
	return addresses, nil
}

func TestResolveSafetyPolicy(t *testing.T) {
	resolver := staticResolver{
		"public.example":       {netip.MustParseAddr("93.184.216.34")},
		"private.example":      {netip.MustParseAddr("10.0.0.2")},
		"loopback.example":     {netip.MustParseAddr("127.0.0.1")},
		"metadata.example":     {netip.MustParseAddr("169.254.169.254")},
		"mixed.example":        {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.2")},
		"demo-target":          {netip.MustParseAddr("172.20.0.4")},
		"other-demo-host.test": {netip.MustParseAddr("172.20.0.5")},
	}
	production := Policy{Resolver: resolver}
	if _, err := production.Resolve(context.Background(), "https", "public.example", 443); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"private.example", "loopback.example", "metadata.example", "mixed.example"} {
		if _, err := production.Resolve(context.Background(), "https", host, 443); !errors.Is(err, ErrUnsafeDestination) {
			t.Fatalf("%s: got %v, want unsafe destination", host, err)
		}
	}

	demo := Policy{Resolver: resolver, DemoMode: true, DemoAllowedOrigin: "http://demo-target:9000"}
	if _, err := demo.Resolve(context.Background(), "http", "demo-target", 9000); err != nil {
		t.Fatalf("exact demo fixture should be allowed: %v", err)
	}
	if _, err := demo.Resolve(context.Background(), "http", "other-demo-host.test", 9000); !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("other private host should remain blocked: %v", err)
	}
	if _, err := demo.Resolve(context.Background(), "http", "loopback.example", 9000); !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("loopback should remain blocked in demo mode: %v", err)
	}
}
