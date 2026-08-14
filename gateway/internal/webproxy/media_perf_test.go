package webproxy

import (
	"bufio"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPooledTransportKeepsAutomaticGzipForRewritablePages(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Errorf("pooled transport did not negotiate gzip: %q", r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Encoding", "gzip")
		compressed := gzip.NewWriter(w)
		_, _ = io.WriteString(compressed, `<!doctype html><html><head></head><body><a href="/next">next</a></body></html>`)
		_ = compressed.Close()
	}))
	defer upstream.Close()

	origin := mustURL(t, upstream.URL)
	proxy := New(Config{DemoAllowedOrigin: upstream.URL})
	request := httptest.NewRequest(http.MethodGet, browsePrefix+encodeOrigin(origin)+"/page", nil)
	request.Header.Set("Accept-Encoding", "br, gzip")
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q after HTML rewrite", got)
	}
	if !strings.Contains(recorder.Body.String(), proxyURL(mustURL(t, upstream.URL+"/next"))) {
		t.Fatalf("decompressed HTML was not rewritten: %s", recorder.Body.String())
	}
	if got, want := recorder.Header().Get("Content-Length"), strconv.Itoa(recorder.Body.Len()); got != want {
		t.Fatalf("rewritten Content-Length = %q, want %q", got, want)
	}
}

func TestProxyForwardsRangeAndPreservesPartialResponseMetadata(t *testing.T) {
	payload := strings.Repeat("R", 1024)
	type observation struct {
		rangeHeader   string
		ifRangeHeader string
	}
	observed := make(chan observation, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- observation{rangeHeader: r.Header.Get("Range"), ifRangeHeader: r.Header.Get("If-Range")}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 4096-5119/1048576")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"video-v1"`)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, payload)
	}))
	defer upstream.Close()

	origin := mustURL(t, upstream.URL)
	proxy := New(Config{DemoAllowedOrigin: upstream.URL})
	request := httptest.NewRequest(http.MethodGet, browsePrefix+encodeOrigin(origin)+"/video.m4s", nil)
	request.Header.Set("Range", "bytes=4096-5119")
	request.Header.Set("If-Range", `"video-v1"`)
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206; body=%q", recorder.Code, recorder.Body.String())
	}
	gotRequest := <-observed
	if gotRequest.rangeHeader != "bytes=4096-5119" || gotRequest.ifRangeHeader != `"video-v1"` {
		t.Fatalf("range request headers changed upstream: %#v", gotRequest)
	}
	for name, want := range map[string]string{
		"Content-Range":     "bytes 4096-5119/1048576",
		"Content-Length":    strconv.Itoa(len(payload)),
		"Accept-Ranges":     "bytes",
		"ETag":              `"video-v1"`,
		"X-Accel-Buffering": "no",
	} {
		if got := recorder.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if recorder.Header().Get("X-OWU-Cache") != "" || recorder.Header().Get("X-Accel-Expires") != "" {
		t.Fatalf("partial response was marked for shared cache: %#v", recorder.Header())
	}
	if recorder.Body.String() != payload {
		t.Fatalf("partial body length = %d, want %d", recorder.Body.Len(), len(payload))
	}
}

func TestRewrittenPublicCSSIsCacheableButHTMLIsNot(t *testing.T) {
	target := mustURL(t, "https://static.example/assets/site.css")
	token := encodeOrigin(target)
	request := httptest.NewRequest(http.MethodGet, "https://owu.example"+proxyURL(target), nil)
	css := `.hero{background-image:url('/images/hero.webp')}`
	response := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(css)),
		ContentLength: int64(len(css)),
	}
	response.Header.Set("Content-Type", "text/css")
	response.Header.Set("Content-Length", strconv.Itoa(len(css)))
	response.Header.Set("Cache-Control", "public, max-age=3600")
	response.Header.Set("ETag", `"source-css"`)
	recorder := httptest.NewRecorder()
	if err := writeProxyResponseWithCache(recorder, request, response, target, token, 15*time.Minute); err != nil {
		t.Fatal(err)
	}

	if got := recorder.Header().Get("X-OWU-Cache"); got != "public-media" {
		t.Fatalf("rewritten CSS cache marker = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, max-age=900" {
		t.Fatalf("rewritten CSS Cache-Control = %q", got)
	}
	if got := recorder.Header().Get("ETag"); got != "" {
		t.Fatalf("source ETag survived body rewrite: %q", got)
	}
	if !strings.Contains(recorder.Body.String(), proxyURL(mustURL(t, "https://static.example/images/hero.webp"))) {
		t.Fatalf("CSS URL was not rewritten: %s", recorder.Body.String())
	}
	if got, want := recorder.Header().Get("Content-Length"), strconv.Itoa(recorder.Body.Len()); got != want {
		t.Fatalf("rewritten CSS Content-Length = %q, want %q", got, want)
	}

	htmlTarget := mustURL(t, "https://static.example/index.html")
	htmlToken := encodeOrigin(htmlTarget)
	htmlRequest := httptest.NewRequest(http.MethodGet, "https://owu.example"+proxyURL(htmlTarget), nil)
	html := `<!doctype html><html><head></head><body>page</body></html>`
	htmlResponse := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(html)),
		ContentLength: int64(len(html)),
	}
	htmlResponse.Header.Set("Content-Type", "text/html; charset=utf-8")
	htmlResponse.Header.Set("Cache-Control", "public, max-age=3600")
	htmlRecorder := httptest.NewRecorder()
	if err := writeProxyResponseWithCache(htmlRecorder, htmlRequest, htmlResponse, htmlTarget, htmlToken, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	if htmlRecorder.Header().Get("X-OWU-Cache") != "" || htmlRecorder.Header().Get("X-Accel-Expires") != "" {
		t.Fatal("rewritten HTML was marked for shared cache")
	}
	if got := htmlRecorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("rewritten HTML Cache-Control = %q", got)
	}
}

func TestTransportPoolReusesConnections(t *testing.T) {
	var newConnections atomic.Int32
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "2")
		_, _ = io.WriteString(w, "ok")
	}))
	upstream.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnections.Add(1)
		}
	}
	upstream.Start()
	defer upstream.Close()

	origin := mustURL(t, upstream.URL)
	proxy := New(Config{DemoAllowedOrigin: upstream.URL})
	for index := 0; index < 3; index++ {
		request := httptest.NewRequest(http.MethodGet, browsePrefix+encodeOrigin(origin)+"/asset.bin?request="+strconv.Itoa(index), nil)
		recorder := httptest.NewRecorder()
		proxy.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
			t.Fatalf("request %d: status=%d body=%q", index, recorder.Code, recorder.Body.String())
		}
	}
	if got := newConnections.Load(); got != 1 {
		t.Fatalf("upstream TCP connections = %d, want one reused keep-alive connection", got)
	}
}

func TestTransportPoolIsBoundedAndUsesLRUEviction(t *testing.T) {
	proxy := New(Config{TransportPoolSize: 2})
	first := mustURL(t, "https://one.example/video.mp4")
	second := mustURL(t, "https://two.example/video.mp4")
	third := mustURL(t, "https://three.example/video.mp4")

	firstTransport := proxy.transportFor(first)
	_ = proxy.transportFor(second)
	if got := proxy.transportFor(first); got != firstTransport {
		t.Fatal("recently used origin did not reuse its transport")
	}
	_ = proxy.transportFor(third)

	proxy.transportMu.Lock()
	defer proxy.transportMu.Unlock()
	if len(proxy.transports) != 2 {
		t.Fatalf("transport pool size = %d, want 2", len(proxy.transports))
	}
	if proxy.transports["https://one.example"] == nil || proxy.transports["https://three.example"] == nil {
		t.Fatalf("LRU retained the wrong origins: %#v", proxy.transports)
	}
	if proxy.transports["https://two.example"] != nil {
		t.Fatal("least-recently-used transport was not evicted")
	}
}

func TestPublicMediaCacheSignalsAreBoundedAndUnforgeable(t *testing.T) {
	target := mustURL(t, "https://video.example/content/segment.m4s")
	token := encodeOrigin(target)
	newRequest := func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "https://owu.example"+proxyURL(target), nil)
	}
	newResponse := func() *http.Response {
		response := &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader("media")),
			ContentLength: 5,
		}
		response.Header.Set("Content-Type", "video/mp4")
		response.Header.Set("Content-Length", "5")
		response.Header.Set("Cache-Control", "public, max-age=86400, immutable")
		return response
	}

	t.Run("eligible response", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		if err := writeProxyResponseWithCache(recorder, newRequest(), newResponse(), target, token, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "private, max-age=900, immutable" {
			t.Fatalf("Cache-Control = %q", got)
		}
		if got := recorder.Header().Get("X-OWU-Cache"); got != "public-media" {
			t.Fatalf("X-OWU-Cache = %q", got)
		}
		if got := recorder.Header().Get("X-Accel-Expires"); got != "900" {
			t.Fatalf("X-Accel-Expires = %q", got)
		}
		if got := recorder.Header().Get("X-Accel-Buffering"); got != "" {
			t.Fatalf("cacheable full media unexpectedly disabled buffering: %q", got)
		}
	})

	t.Run("target cannot forge cache permission", func(t *testing.T) {
		response := newResponse()
		response.Header.Set("Cache-Control", "private, no-store")
		response.Header.Set("X-OWU-Cache", "public-media")
		response.Header.Set("X-Accel-Expires", "86400")
		response.Header.Set("X-Accel-Redirect", "/private-file")
		recorder := httptest.NewRecorder()
		if err := writeProxyResponseWithCache(recorder, newRequest(), response, target, token, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"X-OWU-Cache", "X-Accel-Expires", "X-Accel-Redirect"} {
			if got := recorder.Header().Get(name); got != "" {
				t.Fatalf("forged %s survived: %q", name, got)
			}
		}
		if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("Cache-Control = %q", got)
		}
		if got := recorder.Header().Get("X-Accel-Buffering"); got != "no" {
			t.Fatalf("uncached media buffering = %q, want no", got)
		}
	})

	t.Run("unrelated origin cookie does not disable cache", func(t *testing.T) {
		request := newRequest()
		request.AddCookie(&http.Cookie{Name: cookiePrefix("another-origin") + "session", Value: "unrelated"})
		recorder := httptest.NewRecorder()
		if err := writeProxyResponseWithCache(recorder, request, newResponse(), target, token, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		if got := recorder.Header().Get("X-OWU-Cache"); got != "public-media" {
			t.Fatalf("unrelated cookie disabled media cache: %q", got)
		}
	})

	t.Run("target origin cookie disables cache", func(t *testing.T) {
		request := newRequest()
		request.AddCookie(&http.Cookie{Name: cookiePrefix(token) + "session", Value: "private"})
		recorder := httptest.NewRecorder()
		if err := writeProxyResponseWithCache(recorder, request, newResponse(), target, token, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		if recorder.Header().Get("X-OWU-Cache") != "" || recorder.Header().Get("X-Accel-Expires") != "" {
			t.Fatal("credentialed media was marked for shared cache")
		}
		if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("Cache-Control = %q", got)
		}
	})

	t.Run("set-cookie disables cache", func(t *testing.T) {
		response := newResponse()
		response.Header.Add("Set-Cookie", "session=new; Path=/; HttpOnly")
		recorder := httptest.NewRecorder()
		if err := writeProxyResponseWithCache(recorder, newRequest(), response, target, token, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		if recorder.Header().Get("X-OWU-Cache") != "" {
			t.Fatal("Set-Cookie response was marked for shared cache")
		}
	})

	t.Run("attachment disables browser and shared cache", func(t *testing.T) {
		response := newResponse()
		response.Header.Set("Content-Disposition", `attachment; filename="video.mp4"`)
		recorder := httptest.NewRecorder()
		if err := writeProxyResponseWithCache(recorder, newRequest(), response, target, token, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		if recorder.Header().Get("X-OWU-Cache") != "" || recorder.Header().Get("X-Accel-Expires") != "" {
			t.Fatal("attachment was marked for shared cache")
		}
		if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("attachment Cache-Control = %q", got)
		}
	})

	t.Run("varying representation is browser-private only", func(t *testing.T) {
		response := newResponse()
		response.Header.Set("Vary", "Accept")
		recorder := httptest.NewRecorder()
		if err := writeProxyResponseWithCache(recorder, newRequest(), response, target, token, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		if recorder.Header().Get("X-OWU-Cache") != "" {
			t.Fatal("Vary: Accept response was marked for shared cache")
		}
		if got := recorder.Header().Get("Cache-Control"); got != "private, max-age=900, immutable" {
			t.Fatalf("browser cache policy = %q", got)
		}
	})

	t.Run("s-maxage does not extend browser freshness", func(t *testing.T) {
		response := newResponse()
		response.Header.Set("Cache-Control", "public, max-age=0, s-maxage=600")
		recorder := httptest.NewRecorder()
		if err := writeProxyResponseWithCache(recorder, newRequest(), response, target, token, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		if got := recorder.Header().Get("X-Accel-Expires"); got != "600" {
			t.Fatalf("shared cache TTL = %q, want 600", got)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("browser cache policy = %q", got)
		}
	})

	t.Run("undeclared freshness is not cached", func(t *testing.T) {
		response := newResponse()
		response.Header.Del("Cache-Control")
		recorder := httptest.NewRecorder()
		if err := writeProxyResponseWithCache(recorder, newRequest(), response, target, token, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		if recorder.Header().Get("X-OWU-Cache") != "" || recorder.Header().Get("X-Accel-Expires") != "" {
			t.Fatal("media without declared freshness was marked for cache")
		}
	})

	for _, cacheControl := range []string{
		"public, max-age=0, max-age=3600",
		"public, max-age=3600, s-maxage=0, s-maxage=600",
	} {
		cacheControl := cacheControl
		t.Run("duplicate freshness directives are not cached "+cacheControl, func(t *testing.T) {
			response := newResponse()
			response.Header.Set("Cache-Control", cacheControl)
			recorder := httptest.NewRecorder()
			if err := writeProxyResponseWithCache(recorder, newRequest(), response, target, token, 15*time.Minute); err != nil {
				t.Fatal(err)
			}
			if recorder.Header().Get("X-OWU-Cache") != "" || recorder.Header().Get("X-Accel-Expires") != "" {
				t.Fatalf("ambiguous freshness was marked for shared cache: %#v", recorder.Header())
			}
			if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("ambiguous freshness browser policy = %q", got)
			}
		})
	}

	t.Run("upstream age reduces shared freshness", func(t *testing.T) {
		response := newResponse()
		response.Header.Set("Cache-Control", "public, max-age=900")
		response.Header.Set("Age", "850")
		recorder := httptest.NewRecorder()
		if err := writeProxyResponseWithCache(recorder, newRequest(), response, target, token, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		if got := recorder.Header().Get("X-Accel-Expires"); got != "50" {
			t.Fatalf("remaining shared cache TTL = %q, want 50", got)
		}
	})
}

func TestManifestCacheTTLIsShort(t *testing.T) {
	target := mustURL(t, "https://video.example/live/index.m3u8")
	token := encodeOrigin(target)
	request := httptest.NewRequest(http.MethodGet, "https://owu.example"+proxyURL(target), nil)
	response := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader("#EXTM3U")),
		ContentLength: 7,
	}
	response.Header.Set("Content-Type", "application/vnd.apple.mpegurl")
	response.Header.Set("Content-Length", "7")
	response.Header.Set("Cache-Control", "public, max-age=600")
	recorder := httptest.NewRecorder()
	if err := writeProxyResponseWithCache(recorder, request, response, target, token, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Header().Get("X-Accel-Expires"); got != "60" {
		t.Fatalf("manifest X-Accel-Expires = %q, want 60", got)
	}
}

func TestSSEFlushesBeforeUpstreamCompletes(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: second\n\n")
	}))
	defer upstream.Close()

	origin, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(New(Config{DemoAllowedOrigin: upstream.URL}))
	defer proxy.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.URL+browsePrefix+encodeOrigin(origin)+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		response *http.Response
		err      error
	}
	resultChannel := make(chan result, 1)
	go func() {
		response, requestErr := http.DefaultClient.Do(request)
		resultChannel <- result{response: response, err: requestErr}
	}()

	var response *http.Response
	select {
	case got := <-resultChannel:
		if got.err != nil {
			close(release)
			t.Fatal(got.err)
		}
		response = got.response
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("SSE response headers/first event were buffered until completion")
	}
	defer response.Body.Close()
	if got := response.Header.Get("X-Accel-Buffering"); got != "no" {
		close(release)
		t.Fatalf("X-Accel-Buffering = %q", got)
	}
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "data: first\n" {
		close(release)
		t.Fatalf("first streamed event = %q, err=%v", line, err)
	}
	close(release)
}
