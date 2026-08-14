package webproxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestRewriteHLSManifestRewritesSignedAndRelativeReferences(t *testing.T) {
	base := mustManifestURL(t, "https://live.douyin.com/hls/channel/master.m3u8?session=manifest")
	bilibiliVideo := "https://upos-sz-mirrorcos.bilivideo.com/upgcxcode/12/34/987654321-1-30080.m4s?e=ig8euxZM2rNcNbRbhwdVhoM17wUVhwdEto8g5X10ugNcXBB_&uipk=5&nbs=1&deadline=1999999999&gen=playurlv2&os=cos&oi=1234567890&trid=abc,def"
	douyinKey := "https://v26-web.douyinvod.com/video/tos/cn/tos-cn-ve/key.bin?x-expires=1999999999&x-signature=ab%2FCd%2Bef%3D&byte=1,2"
	alreadyTarget := mustManifestURL(t, "https://media.example.net/already/segment.ts?sig=kept")
	already := proxyURL(alreadyTarget)

	lines := []string{
		"#EXTM3U",
		"# A comment with URI=\"relative/comment.ts\" must stay byte-for-byte",
		"#EXT-X-VERSION:9",
		"#EXT-X-STREAM-INF:BANDWIDTH=4400000,CODECS=\"avc1.640028,mp4a.40.2\"",
		"  video/720p/index.m3u8?auth=alpha%2Bbeta  ",
		"#EXT-X-KEY:METHOD=AES-128,URI=\"" + douyinKey + "\",IV=0x0123",
		"#EXT-X-MAP:BYTERANGE=\"720@0\",URI=\"/init/video.mp4?token=root%2Fpath\"",
		"#EXT-X-PART:DURATION=0.33334,URI=\"../parts/part001.m4s?wsSecret=a,b,c&wsTime=123\",INDEPENDENT=YES",
		"#EXT-X-PRELOAD-HINT:TYPE=PART,URI=\"//pull-flv-l1.douyincdn.com/live/next.m4s?expire=123&sign=x%2Fy\"",
		"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aac\",NAME=\"main, stereo\",URI=\"audio/prog.m3u8?token=a,b\"",
		"#EXT-X-CONTENT-STEERING:SERVER-URI=\"https://steering.example.net/api?v=1,2&sig=x%2By\",PATHWAY-ID=\"cdn-a\"",
		"#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=86000,URI=\"" + bilibiliVideo + "\"",
		"#EXT-X-MAP:URI=\"data:video/mp4;base64,AAAA,BBBB\"",
		"#EXT-X-RENDITION-REPORT:URI=\"blob:https://live.douyin.com/deadbeef\",LAST-MSN=12",
		already,
		"https://owu.example" + already,
	}
	input := strings.Join(lines, "\r\n")

	expectedLines := append([]string(nil), lines...)
	expectedLines[4] = "  " + proxyReference(t, base, "video/720p/index.m3u8?auth=alpha%2Bbeta") + "  "
	expectedLines[5] = "#EXT-X-KEY:METHOD=AES-128,URI=\"" + proxyReference(t, base, douyinKey) + "\",IV=0x0123"
	expectedLines[6] = "#EXT-X-MAP:BYTERANGE=\"720@0\",URI=\"" + proxyReference(t, base, "/init/video.mp4?token=root%2Fpath") + "\""
	expectedLines[7] = "#EXT-X-PART:DURATION=0.33334,URI=\"" + proxyReference(t, base, "../parts/part001.m4s?wsSecret=a,b,c&wsTime=123") + "\",INDEPENDENT=YES"
	expectedLines[8] = "#EXT-X-PRELOAD-HINT:TYPE=PART,URI=\"" + proxyReference(t, base, "//pull-flv-l1.douyincdn.com/live/next.m4s?expire=123&sign=x%2Fy") + "\""
	expectedLines[9] = "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aac\",NAME=\"main, stereo\",URI=\"" + proxyReference(t, base, "audio/prog.m3u8?token=a,b") + "\""
	expectedLines[10] = "#EXT-X-CONTENT-STEERING:SERVER-URI=\"" + proxyReference(t, base, "https://steering.example.net/api?v=1,2&sig=x%2By") + "\",PATHWAY-ID=\"cdn-a\""
	expectedLines[11] = "#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=86000,URI=\"" + proxyReference(t, base, bilibiliVideo) + "\""
	expectedLines[14] = proxyReference(t, base, lines[14])
	expectedLines[15] = proxyReference(t, base, lines[15])
	expected := strings.Join(expectedLines, "\r\n")

	rewritten := rewriteHLSManifest([]byte(input), base)
	if got := string(rewritten); got != expected {
		t.Fatalf("rewritten HLS mismatch\n--- got ---\n%s\n--- want ---\n%s", got, expected)
	}
	if bytes.Contains(bytes.ReplaceAll(rewritten, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatal("rewriter introduced a bare LF into a CRLF playlist")
	}
}

func TestManifestAbsoluteBrowseShapedURLIsStillProxied(t *testing.T) {
	base := mustManifestURL(t, "https://media.example.net/live/master.m3u8")
	foreignOrigin := mustManifestURL(t, "https://target.example")
	foreign := "https://attacker.example" + proxyURL(foreignOrigin) + "/segment.ts"
	rewritten := string(rewriteHLSManifest([]byte("#EXTM3U\n"+foreign+"\n"), base))
	want := proxyReference(t, base, foreign)
	if !strings.Contains(rewritten, want) {
		t.Fatalf("absolute browse-shaped URL bypassed OWU: got %q, want %q", rewritten, want)
	}
	if strings.Contains(rewritten, "\n"+foreign+"\n") {
		t.Fatalf("absolute browse-shaped URL remained a direct browser target: %q", rewritten)
	}
}

func TestRewriteHLSManifestSupportsUnquotedURIAndPreservesLineEndings(t *testing.T) {
	base := mustManifestURL(t, "http://47.83.130.57:9020/live/playlist.m3u8")
	input := "#EXTM3U\n#EXT-X-MAP:URI=init.mp4?token=a%2Fb,FOO=bar\r#EXTINF:6,\nsegment-01.ts?sign=x,y"
	expected := "#EXTM3U\n#EXT-X-MAP:URI=" + proxyReference(t, base, "init.mp4?token=a%2Fb") + ",FOO=bar\r#EXTINF:6,\n" + proxyReference(t, base, "segment-01.ts?sign=x,y")
	if got := string(rewriteHLSManifest([]byte(input), base)); got != expected {
		t.Fatalf("mixed line-ending rewrite mismatch\ngot:  %q\nwant: %q", got, expected)
	}
}

func TestRewriteDASHManifestPreservesXMLAndBaseURLInheritance(t *testing.T) {
	base := mustManifestURL(t, "https://www.bilibili.com/dash/manifest.mpd?qn=80")
	input := `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" xmlns:xlink="http://www.w3.org/1999/xlink">
  <BaseURL serviceLocation="cos">https://upos-sz-mirrorcos.bilivideo.com/upgcxcode/root/?deadline=1999999999&amp;sign=a%2Fb</BaseURL>
  <Location>https://api.bilibili.com/x/player/mpd?id=42&amp;qn=80</Location>
  <Period xlink:href="/dash/period.xml?token=a%2Bb&amp;part=1">
    <AdaptationSet>
      <Representation id="v1">
        <SegmentTemplate initialization="/dash/init-$RepresentationID$.m4s?sig=x%2Fy" media="video/$Number$.m4s?token=a&amp;b=c" />
        <SegmentURL media="https://v26-web.douyinvod.com/video/seg.m4s?x-signature=a%2Fb&amp;range=1,2" index="index.sidx" />
        <Initialization sourceURL="data:video/mp4;base64,AAAA" />
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`

	rewritten, err := rewriteDASHManifest([]byte(input), base)
	if err != nil {
		t.Fatalf("rewrite DASH: %v", err)
	}
	if err := validateXML(rewritten); err != nil {
		t.Fatalf("rewritten DASH is invalid XML: %v\n%s", err, rewritten)
	}

	expectedBase := xmlText(proxyReference(t, base, "https://upos-sz-mirrorcos.bilivideo.com/upgcxcode/root/?deadline=1999999999&sign=a%2Fb"))
	expectedLocation := xmlText(proxyReference(t, base, "https://api.bilibili.com/x/player/mpd?id=42&qn=80"))
	expectedPeriod := xmlAttribute(proxyReference(t, base, "/dash/period.xml?token=a%2Bb&part=1"))
	expectedInit := xmlAttribute(proxyReference(t, base, "/dash/init-$RepresentationID$.m4s?sig=x%2Fy"))
	expectedSegment := xmlAttribute(proxyReference(t, base, "https://v26-web.douyinvod.com/video/seg.m4s?x-signature=a%2Fb&range=1,2"))
	got := string(rewritten)
	for _, want := range []string{expectedBase, expectedLocation, `xlink:href="` + expectedPeriod + `"`, `initialization="` + expectedInit + `"`, `media="` + expectedSegment + `"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten DASH does not contain %q\n%s", want, got)
		}
	}
	for _, preserved := range []string{`media="video/$Number$.m4s?token=a&amp;b=c"`, `index="index.sidx"`, `sourceURL="data:video/mp4;base64,AAAA"`} {
		if !strings.Contains(got, preserved) {
			t.Errorf("BaseURL-relative or non-network DASH value changed: missing %q\n%s", preserved, got)
		}
	}
}

func TestRewriteDASHManifestRewritesRelativeTemplatesWithoutBaseHierarchy(t *testing.T) {
	base := mustManifestURL(t, "https://media.example.com/path/manifest.mpd")
	input := `<MPD><Period><AdaptationSet><Representation><SegmentTemplate media="../video/$Number$.m4s?token=a&amp;b=c" initialization="init-$RepresentationID$.m4s" /></Representation></AdaptationSet></Period></MPD>`
	rewritten, err := rewriteDASHManifest([]byte(input), base)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`media="` + xmlAttribute(proxyReference(t, base, "../video/$Number$.m4s?token=a&b=c")) + `"`,
		`initialization="` + xmlAttribute(proxyReference(t, base, "init-$RepresentationID$.m4s")) + `"`,
	} {
		if !strings.Contains(string(rewritten), want) {
			t.Errorf("relative DASH URL not rewritten: missing %q in %s", want, rewritten)
		}
	}
}

func TestRewriteDASHManifestRejectsMalformedXML(t *testing.T) {
	base := mustManifestURL(t, "https://media.example.com/manifest.mpd")
	if rewritten, err := rewriteDASHManifest([]byte(`<MPD><BaseURL>https://cdn.example/x`), base); err == nil || rewritten != nil {
		t.Fatalf("expected malformed XML error, got body=%q err=%v", rewritten, err)
	}
}

func TestProxyRewritesAndCachesPublicHLSManifest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("ETag", `"manifest-v1"`)
		_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:4,\nsegments/part-001.m4s?sign=a%2Bb\n")
	}))
	defer upstream.Close()

	origin := mustManifestURL(t, upstream.URL)
	proxy := New(Config{DemoAllowedOrigin: upstream.URL})
	request := httptest.NewRequest(http.MethodGet, browsePrefix+encodeOrigin(origin)+"/live/master.m3u8", nil)
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	wantSegment := proxyReference(t, mustManifestURL(t, upstream.URL+"/live/master.m3u8"), "segments/part-001.m4s?sign=a%2Bb")
	if !strings.Contains(recorder.Body.String(), wantSegment) {
		t.Fatalf("HLS response missing proxied segment %q: %s", wantSegment, recorder.Body.String())
	}
	for name, want := range map[string]string{
		"Cache-Control":   "private, max-age=60",
		"X-OWU-Cache":     "public-media",
		"X-Accel-Expires": "60",
		"Content-Length":  strconv.Itoa(recorder.Body.Len()),
	} {
		if got := recorder.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := recorder.Header().Get("ETag"); got != "" {
		t.Fatalf("rewritten manifest retained ETag %q", got)
	}
}

func TestManifestPathDoesNotOverrideHTMLContentType(t *testing.T) {
	if isHLSManifestContent("text/html; charset=utf-8", "/challenge.m3u8") {
		t.Fatal("HTML bot challenge was classified as HLS")
	}
	if isDASHManifestContent("text/html; charset=utf-8", "/challenge.mpd") {
		t.Fatal("HTML bot challenge was classified as DASH")
	}
}

func mustManifestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func proxyReference(t *testing.T, base *url.URL, reference string) string {
	t.Helper()
	resolved, err := base.Parse(reference)
	if err != nil {
		t.Fatalf("resolve %q: %v", reference, err)
	}
	return proxyURL(resolved)
}

func xmlText(value string) string {
	return escapeXMLText(value)
}

func xmlAttribute(value string) string {
	return escapeXMLAttribute(value, '"')
}
