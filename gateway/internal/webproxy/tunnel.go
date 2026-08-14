package webproxy

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	tunnelKeyHeader    = "X-OWU-Tunnel-Key"
	maximumTunnelFrame = 1 << 20
)

var tunnelResourceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// TCPResource is an operator-configured destination. The client sends only the
// resource ID; host and port are never accepted from a request.
type TCPResource struct {
	ID   string
	Host string
	Port uint16
}

func (r TCPResource) Address() string {
	return net.JoinHostPort(r.Host, strconv.Itoa(int(r.Port)))
}

// parseTCPResources parses: ssh=127.0.0.1:22,minecraft=mc.example.com:25565
func parseTCPResources(value string) (map[string]TCPResource, error) {
	resources := make(map[string]TCPResource)
	value = strings.TrimSpace(value)
	if value == "" {
		return resources, nil
	}
	for _, entry := range strings.Split(value, ",") {
		id, address, found := strings.Cut(strings.TrimSpace(entry), "=")
		id = strings.ToLower(strings.TrimSpace(id))
		address = strings.TrimSpace(address)
		if !found || !tunnelResourceIDPattern.MatchString(id) {
			return nil, fmt.Errorf("invalid resource entry %q", entry)
		}
		if _, exists := resources[id]; exists {
			return nil, fmt.Errorf("duplicate resource %q", id)
		}
		host, portText, err := net.SplitHostPort(address)
		if err != nil || strings.TrimSpace(host) == "" {
			return nil, fmt.Errorf("resource %q must use host:port", id)
		}
		port, err := strconv.ParseUint(portText, 10, 16)
		if err != nil || port == 0 {
			return nil, fmt.Errorf("resource %q has an invalid port", id)
		}
		resources[id] = TCPResource{ID: id, Host: strings.TrimSuffix(strings.TrimSpace(host), "."), Port: uint16(port)}
	}
	return resources, nil
}

func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "This request method is not supported.")
		return
	}
	if len(s.tunnelKey) < 20 || !constantTimeStringEqual(r.Header.Get(tunnelKeyHeader), s.tunnelKey) {
		writeError(w, http.StatusUnauthorized, "The tunnel credential is invalid.")
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, tunnelPrefix), "/")
	if !tunnelResourceIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "The tunnel resource is invalid.")
		return
	}
	resource, ok := s.tunnelResources[id]
	if !ok {
		writeError(w, http.StatusNotFound, "The tunnel resource is not configured.")
		return
	}
	s.recordUse(r, "", "")

	dialContext, cancelDial := context.WithTimeout(r.Context(), 10*time.Second)
	upstream, err := (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(dialContext, "tcp", resource.Address())
	cancelDial()
	if err != nil {
		writeError(w, http.StatusBadGateway, "The tunnel destination did not respond.")
		return
	}
	defer upstream.Close()

	websocketConnection, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer websocketConnection.CloseNow()
	websocketConnection.SetReadLimit(maximumTunnelFrame)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := websocket.NetConn(ctx, websocketConnection, websocket.MessageBinary)
	defer stream.Close()

	var uploaded, downloaded int64
	copyDone := make(chan error, 2)
	go func() {
		_, copyErr := io.Copy(upstream, &countingReader{reader: stream, count: &uploaded})
		if tcp, ok := upstream.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		copyDone <- copyErr
	}()
	go func() {
		_, copyErr := io.Copy(&countingWriter{writer: stream, count: &downloaded}, upstream)
		copyDone <- copyErr
	}()

	first := <-copyDone
	cancel()
	_ = stream.Close()
	second := <-copyDone
	s.recordTraffic(uint64(uploaded + downloaded))
	if !isExpectedTunnelClose(first) && !isExpectedTunnelClose(second) {
		_ = websocketConnection.Close(websocket.StatusInternalError, "tunnel closed")
		return
	}
	_ = websocketConnection.Close(websocket.StatusNormalClosure, "")
}

// countingReader counts bytes pulled from a stream.
type countingReader struct {
	reader io.Reader
	count  *int64
}

func (c *countingReader) Read(payload []byte) (int, error) {
	read, err := c.reader.Read(payload)
	*c.count += int64(read)
	return read, err
}

// countingWriter counts bytes pushed into a stream.
type countingWriter struct {
	writer io.Writer
	count  *int64
}

func (c *countingWriter) Write(payload []byte) (int, error) {
	written, err := c.writer.Write(payload)
	*c.count += int64(written)
	return written, err
}

func constantTimeStringEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func isExpectedTunnelClose(err error) bool {
	return err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled)
}
