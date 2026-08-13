package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	endpoint := strings.TrimSpace(os.Getenv("OWU_SMOKE_URL"))
	username := strings.TrimSpace(os.Getenv("OWU_SMOKE_USERNAME"))
	password := os.Getenv("OWU_SMOKE_PASSWORD")
	if endpoint == "" || username == "" || password == "" {
		fmt.Fprintln(os.Stderr, "OWU_SMOKE_URL, OWU_SMOKE_USERNAME, and OWU_SMOKE_PASSWORD are required")
		os.Exit(2)
	}

	headers := http.Header{}
	headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
	headers.Set("X-OWU-Tunnel-Key", password)
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: os.Getenv("OWU_SMOKE_INSECURE") == "true", // test client for the IP/self-signed deployment
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connection, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPClient: httpClient, HTTPHeader: headers})
	if err != nil {
		if response != nil {
			fmt.Fprintf(os.Stderr, "websocket status=%d: %v\n", response.StatusCode, err)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
	defer connection.CloseNow()
	stream := websocket.NetConn(ctx, connection, websocket.MessageBinary)
	defer stream.Close()
	_ = stream.SetReadDeadline(time.Now().Add(10 * time.Second))
	buffer := make([]byte, 256)
	n, err := stream.Read(buffer)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("tunnel-ok bytes=%d prefix=%q\n", n, strings.TrimSpace(string(buffer[:n])))
}
