package stats

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderPersistenceAndTotals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.json")

	recorder, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer recorder.Close()

	visitorA := recorder.VisitorID("203.0.113.7:53000")
	visitorB := recorder.VisitorID("198.51.100.9:44000")
	if visitorA == visitorB {
		t.Fatal("distinct addresses must produce distinct visitor ids")
	}
	if recorder.VisitorID("203.0.113.7:53000") != visitorA {
		t.Fatal("visitor id must be stable for the same address")
	}

	recorder.Record(visitorA, "example.com", true)
	recorder.Record(visitorA, "example.com", false) // subresource: no use, visitor stays
	recorder.Record(visitorA, "www.example.org", true)
	recorder.Record(visitorB, "example.com", true)
	recorder.AddTraffic(1024)
	recorder.AddTraffic(0) // ignored
	recorder.AddTraffic(512)

	recorder.mu.Lock()
	recorder.dirty = true
	recorder.mu.Unlock()
	if err := recorder.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	reloaded, err := New(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	defer reloaded.Close()

	snapshot := reloaded.Snapshot()
	if snapshot.UsesTotal != 3 {
		t.Fatalf("UsesTotal = %d, want 3", snapshot.UsesTotal)
	}
	if snapshot.UsesToday != 3 {
		t.Fatalf("UsesToday = %d, want 3", snapshot.UsesToday)
	}
	if snapshot.TrafficTotal != 1536 {
		t.Fatalf("TrafficTotal = %d, want 1536", snapshot.TrafficTotal)
	}
	if snapshot.TrafficToday != 1536 {
		t.Fatalf("TrafficToday = %d, want 1536", snapshot.TrafficToday)
	}
	if got := len(snapshot.Visitors); got != 2 {
		t.Fatalf("unique visitors = %d, want 2", got)
	}
	if got := reloaded.VisitorsToday(snapshot); got != 2 {
		t.Fatalf("visitors today = %d, want 2", got)
	}
	if snapshot.Sites["example.com"] != 2 {
		t.Fatalf("example.com uses = %d, want 2", snapshot.Sites["example.com"])
	}
	if snapshot.Sites["www.example.org"] != 1 {
		t.Fatalf("www.example.org uses = %d, want 1", snapshot.Sites["www.example.org"])
	}
	if snapshot.Since.IsZero() {
		t.Fatal("Since must be set")
	}
}

func TestRecorderDayRollover(t *testing.T) {
	dir := t.TempDir()
	recorder, err := New(filepath.Join(dir, "stats.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer recorder.Close()

	visitor := recorder.VisitorID("203.0.113.7:53000")
	recorder.Record(visitor, "example.com", true)
	recorder.AddTraffic(1000)

	// Simulate a new local day.
	recorder.mu.Lock()
	recorder.data.Day = ""
	recorder.mu.Unlock()

	recorder.Record(visitor, "example.net", true)
	recorder.AddTraffic(200)
	snapshot := recorder.Snapshot()
	if snapshot.UsesToday != 1 {
		t.Fatalf("UsesToday after rollover = %d, want 1", snapshot.UsesToday)
	}
	if snapshot.UsesTotal != 2 {
		t.Fatalf("UsesTotal = %d, want 2", snapshot.UsesTotal)
	}
	if snapshot.TrafficToday != 200 {
		t.Fatalf("TrafficToday after rollover = %d, want 200", snapshot.TrafficToday)
	}
	if snapshot.TrafficTotal != 1200 {
		t.Fatalf("TrafficTotal = %d, want 1200", snapshot.TrafficTotal)
	}
	if got := recorder.VisitorsToday(snapshot); got != 1 {
		t.Fatalf("VisitorsToday = %d, want 1", got)
	}
}

func TestNormalizeSite(t *testing.T) {
	cases := map[string]string{
		"Example.COM":  "example.com",
		"EXAMPLE.com.": "example.com",
		"  foo.org  ":  "foo.org",
	}
	for input, want := range cases {
		if got := NormalizeSite(input); got != want {
			t.Fatalf("NormalizeSite(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNewRejectsEmptyPath(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("New(\"\") must fail")
	}
}

func TestRecorderSaveCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "owu")
	recorder, err := New(filepath.Join(dir, "stats.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	recorder.Record(recorder.VisitorID("127.0.0.1:1"), "example.com", true)
	recorder.mu.Lock()
	recorder.dirty = true
	recorder.mu.Unlock()
	if err := recorder.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stats.json")); err != nil {
		t.Fatalf("stats file missing after save: %v", err)
	}
	_ = time.Now()
}
