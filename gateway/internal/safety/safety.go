package safety

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

var ErrUnsafeDestination = errors.New("destination resolved to a prohibited network address")

type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type NetResolver struct{ Resolver *net.Resolver }

func (r NetResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	resolver := r.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return resolver.LookupNetIP(ctx, network, host)
}

type Policy struct {
	Resolver          Resolver
	DemoMode          bool
	DemoAllowedOrigin string
}

func (p Policy) Resolve(ctx context.Context, scheme, host string, port int) ([]netip.Addr, error) {
	resolver := p.Resolver
	if resolver == nil {
		resolver = NetResolver{}
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve target: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("target hostname has no addresses")
	}
	demoOrigin := scheme + "://" + host
	if !((scheme == "http" && port == 80) || (scheme == "https" && port == 443)) {
		demoOrigin += ":" + strconv.Itoa(port)
	}
	demoException := p.DemoMode && demoOrigin == p.DemoAllowedOrigin

	checked := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if prohibited(address) && !(demoException && address.IsPrivate()) {
			return nil, fmt.Errorf("%w: %s", ErrUnsafeDestination, classify(address))
		}
		checked = append(checked, address)
	}
	sort.Slice(checked, func(i, j int) bool { return checked[i].Compare(checked[j]) < 0 })
	return checked, nil
}

func (p Policy) DialContext(ctx context.Context, network, address, scheme, expectedHost string, expectedPort int) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil || !strings.EqualFold(strings.TrimSuffix(host, "."), expectedHost) || portText != strconv.Itoa(expectedPort) {
		return nil, errors.New("upstream dial target did not match the authorized resource")
	}
	addresses, err := p.Resolve(ctx, scheme, expectedHost, expectedPort)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{}
	var errs []error
	for _, ip := range addresses {
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), portText))
		if dialErr == nil {
			return connection, nil
		}
		errs = append(errs, dialErr)
	}
	return nil, errors.Join(errs...)
}

func prohibited(address netip.Addr) bool {
	return !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() || isSpecialUse(address)
}

func isSpecialUse(address netip.Addr) bool {
	for _, prefix := range specialUsePrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

var specialUsePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func classify(address netip.Addr) string {
	switch {
	case address.IsLoopback():
		return "loopback"
	case address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast():
		return "link-local"
	case address.IsPrivate():
		return "private"
	case address.IsMulticast():
		return "multicast"
	case address.IsUnspecified():
		return "unspecified"
	default:
		return "non-public"
	}
}

// IsConnectionRefused makes integration-test failures easier to classify without
// exposing the resolved address to an API caller.
func IsConnectionRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
