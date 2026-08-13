package urlpolicy

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

const MaxInputBytes = 2048

type Code string

const (
	InvalidURL            Code = "INVALID_URL"
	SchemeNotAllowed      Code = "SCHEME_NOT_ALLOWED"
	CredentialsNotAllowed Code = "URL_CREDENTIALS_NOT_ALLOWED"
	IPLiteralNotAllowed   Code = "IP_LITERAL_NOT_ALLOWED"
	PortNotAllowed        Code = "PORT_NOT_ALLOWED"
)

type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return e.Message }

func ErrorCode(err error) Code {
	var parseErr *Error
	if errors.As(err, &parseErr) {
		return parseErr.Code
	}
	return InvalidURL
}

type Normalized struct {
	URL           string `json:"normalized_url"`
	Origin        string `json:"origin"`
	Scheme        string `json:"scheme"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Path          string `json:"path"`
	RawQuery      string `json:"-"`
	Fragment      string `json:"-"`
	EffectiveHost string `json:"-"`
}

func Parse(input string) (Normalized, error) {
	if len(input) > MaxInputBytes {
		return Normalized{}, reject(InvalidURL, "The address is too long.")
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return Normalized{}, reject(InvalidURL, "Enter a website address.")
	}
	if strings.Contains(input, "\\") || hasControl(input) {
		return Normalized{}, reject(InvalidURL, "The address contains prohibited characters.")
	}
	if !validPercentEncoding(input) {
		return Normalized{}, reject(InvalidURL, "The address contains invalid percent encoding.")
	}

	u, err := url.Parse(input)
	if err != nil || u.Opaque != "" || !u.IsAbs() {
		return Normalized{}, reject(InvalidURL, "Enter an absolute HTTP or HTTPS address.")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return Normalized{}, reject(SchemeNotAllowed, "Only HTTP and HTTPS addresses are supported.")
	}
	if u.User != nil {
		return Normalized{}, reject(CredentialsNotAllowed, "Credentials are not allowed in an address.")
	}
	if u.Host == "" || u.Hostname() == "" {
		return Normalized{}, reject(InvalidURL, "The address must contain a hostname.")
	}
	if strings.HasSuffix(u.Host, ":") {
		return Normalized{}, reject(PortNotAllowed, "The port is empty.")
	}

	rawHost := strings.TrimSuffix(u.Hostname(), ".")
	if net.ParseIP(rawHost) != nil {
		return Normalized{}, reject(IPLiteralNotAllowed, "IP address destinations are not accepted.")
	}
	asciiHost, err := idna.Lookup.ToASCII(rawHost)
	if err != nil {
		return Normalized{}, reject(InvalidURL, "The hostname is not valid.")
	}
	asciiHost = strings.ToLower(asciiHost)
	if !validDNSName(asciiHost) {
		return Normalized{}, reject(InvalidURL, "The hostname is not valid.")
	}

	port := 443
	if scheme == "http" {
		port = 80
	}
	if rawPort := u.Port(); rawPort != "" {
		if !allDecimal(rawPort) {
			return Normalized{}, reject(PortNotAllowed, "The port must be a decimal number.")
		}
		parsed, err := strconv.Atoi(rawPort)
		if err != nil || parsed < 1 || parsed > 65535 {
			return Normalized{}, reject(PortNotAllowed, "The port must be between 1 and 65535.")
		}
		port = parsed
	}

	if containsEncodedSeparator(u.EscapedPath()) {
		return Normalized{}, reject(InvalidURL, "Encoded path separators are not accepted.")
	}
	normalizedPath := cleanPath(u.Path)
	hostForURL := asciiHost
	defaultPort := (scheme == "http" && port == 80) || (scheme == "https" && port == 443)
	if !defaultPort {
		hostForURL = net.JoinHostPort(asciiHost, strconv.Itoa(port))
	}

	normalizedURL := &url.URL{
		Scheme:   scheme,
		Host:     hostForURL,
		Path:     normalizedPath,
		RawQuery: u.RawQuery,
		Fragment: u.Fragment,
	}
	origin := scheme + "://" + hostForURL
	return Normalized{
		URL:           normalizedURL.String(),
		Origin:        origin,
		Scheme:        scheme,
		Host:          asciiHost,
		Port:          port,
		Path:          normalizedPath,
		RawQuery:      u.RawQuery,
		Fragment:      u.Fragment,
		EffectiveHost: net.JoinHostPort(asciiHost, strconv.Itoa(port)),
	}, nil
}

func reject(code Code, message string) error { return &Error{Code: code, Message: message} }

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func allDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validDNSName(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}
	return true
}

func containsEncodedSeparator(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "%2f") || strings.Contains(s, "%5c")
}

func validPercentEncoding(s string) bool {
	for index := 0; index < len(s); index++ {
		if s[index] != '%' {
			continue
		}
		if index+2 >= len(s) || !isHex(s[index+1]) || !isHex(s[index+2]) {
			return false
		}
		index += 2
	}
	return true
}

func isHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	trailing := strings.HasSuffix(p, "/")
	p = path.Clean("/" + strings.TrimPrefix(p, "/"))
	if trailing && p != "/" {
		p += "/"
	}
	return p
}

func (n Normalized) TupleKey() string {
	return fmt.Sprintf("%s|%s|%d", n.Scheme, n.Host, n.Port)
}
