package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/netip"
	"strings"
	"time"
)

type auditEvent struct {
	OccurredAt time.Time `json:"occurred_at"`
	RequestID  string    `json:"request_id"`
	ActorID    string    `json:"actor_id,omitempty"`
	ResourceID string    `json:"resource_id,omitempty"`
	Action     string    `json:"action"`
	Method     string    `json:"method,omitempty"`
	PathHash   string    `json:"path_hash,omitempty"`
	Decision   string    `json:"decision"`
	Reason     string    `json:"reason"`
	StatusCode int       `json:"status_code,omitempty"`
	BytesIn    int64     `json:"bytes_in,omitempty"`
	BytesOut   int64     `json:"bytes_out,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	ClientNet  string    `json:"client_ip_prefix,omitempty"`
}

type auditor struct{ logger *log.Logger }

func newAuditor(output io.Writer) auditor {
	if output == nil {
		output = io.Discard
	}
	return auditor{logger: log.New(output, "", 0)}
}

func (a auditor) write(event auditEvent) {
	encoded, err := json.Marshal(event)
	if err == nil {
		a.logger.Print(string(encoded))
	}
}

func pathHash(path string) string {
	digest := sha256.Sum256([]byte(path))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func clientPrefix(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	address, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return "unknown"
	}
	address = address.Unmap()
	bits := 64
	if address.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(address, bits).Masked().String()
}
