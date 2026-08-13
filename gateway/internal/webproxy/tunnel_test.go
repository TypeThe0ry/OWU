package webproxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const testTunnelKey = "test-tunnel-key-that-is-long-enough"

func TestParseTCPResources(t *testing.T) {
	resources, err := parseTCPResources("ssh=127.0.0.1:22,minecraft=[::1]:25565")
	if err != nil {
		t.Fatal(err)
	}
	if resources["ssh"].Port != 22 || resources["minecraft"].Port != 25565 {
		t.Fatalf("unexpected resources: %#v", resources)
	}
	for _, invalid := range []string{"bad id=host:22", "ssh=host:0", "ssh=host", "ssh=host:22,ssh=host:23"} {
		if _, err := parseTCPResources(invalid); err == nil {
			t.Fatalf("expected %q to fail", invalid)
		}
	}
}

func TestTunnelRequiresCredentialAndConfiguredResource(t *testing.T) {
	server := httptest.NewServer(New(Config{
		TunnelKey:       testTunnelKey,
		TunnelResources: map[string]TCPResource{"ssh": {ID: "ssh", Host: "127.0.0.1", Port: 1}},
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/tunnel/ssh")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.StatusCode)
	}

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/tunnel/unknown", nil)
	request.Header.Set(tunnelKeyHeader, testTunnelKey)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.StatusCode)
	}
}

func TestTunnelBridgesBinaryTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
	}()

	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	server := httptest.NewServer(New(Config{
		TunnelKey: testTunnelKey,
		TunnelResources: map[string]TCPResource{
			"echo": {ID: "echo", Host: host, Port: uint16(port)},
		},
	}))
	defer server.Close()

	endpoint, _ := url.Parse(server.URL)
	endpoint.Scheme = "ws"
	endpoint.Path = "/tunnel/echo"
	headers := http.Header{}
	headers.Set(tunnelKeyHeader, testTunnelKey)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	websocketConnection, response, err := websocket.Dial(ctx, endpoint.String(), &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		if response != nil {
			t.Fatalf("dial failed with status %d: %v", response.StatusCode, err)
		}
		t.Fatal(err)
	}
	defer websocketConnection.CloseNow()
	stream := websocket.NetConn(ctx, websocketConnection, websocket.MessageBinary)
	defer stream.Close()

	payload := []byte{0, 1, 2, 3, 0xff, 'O', 'W', 'U'}
	if _, err := stream.Write(payload); err != nil {
		t.Fatal(err)
	}
	result := make([]byte, len(payload))
	if _, err := io.ReadFull(stream, result); err != nil {
		t.Fatal(err)
	}
	if string(result) != string(payload) {
		t.Fatalf("expected %v, got %v", payload, result)
	}
}
