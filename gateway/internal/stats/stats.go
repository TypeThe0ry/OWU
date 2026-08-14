// Package stats records anonymous usage statistics for the OWU proxy.
//
// Visitors are identified only by a salted one-way hash of their client
// address; the raw address is never stored. Usage is counted per request and
// per destination website hostname. State is persisted as JSON so restarts
// keep totals, and a separate salt file keeps visitor hashes stable.
package stats

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Data is the persisted snapshot. Maps are keyed by opaque identifiers only.
type Data struct {
	Since        time.Time         `json:"since"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	UsesTotal    int64             `json:"usesTotal"`
	UsesToday    int64             `json:"usesToday"`
	TrafficTotal uint64            `json:"trafficTotal"` // response + request bytes
	TrafficToday uint64            `json:"trafficToday"`
	Day          string            `json:"day"`      // local date (2006-01-02) UsesToday belongs to
	Visitors     map[string]string `json:"visitors"` // salted visitor hash -> last active local date
	Sites        map[string]int64  `json:"sites"`    // website hostname -> use count
}

// Recorder serializes statistic updates and periodically persists them.
type Recorder struct {
	mu      sync.Mutex
	path    string
	salt    string
	dirty   bool
	stopped chan struct{}
	done    chan struct{}
	data    Data
}

// New loads persisted statistics from path, or starts fresh. The visitor salt
// is read from path+".salt" and created there when missing, so unique-visitor
// identity survives restarts without storing any raw client data.
func New(path string) (*Recorder, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("stats file path is empty")
	}
	salt, err := loadOrCreateSalt(path + ".salt")
	if err != nil {
		return nil, err
	}
	recorder := &Recorder{
		path:    path,
		salt:    salt,
		stopped: make(chan struct{}),
		done:    make(chan struct{}),
	}
	// A recorder that was never started must still Close cleanly; Start()
	// replaces done with a fresh channel that is closed when the flusher exits.
	close(recorder.done)
	if err := recorder.load(); err != nil {
		return nil, err
	}
	return recorder, nil
}

func loadOrCreateSalt(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		salt := strings.TrimSpace(string(raw))
		if salt != "" {
			return salt, nil
		}
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate stats salt: %w", err)
	}
	salt := hex.EncodeToString(bytes[:])
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create stats directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(salt+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write stats salt: %w", err)
	}
	return salt, nil
}

func (r *Recorder) load() error {
	raw, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		r.data = Data{Since: time.Now(), Day: todayKey(), Visitors: map[string]string{}, Sites: map[string]int64{}}
		r.dirty = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("read stats file: %w", err)
	}
	if err := json.Unmarshal(raw, &r.data); err != nil {
		return fmt.Errorf("parse stats file: %w", err)
	}
	if r.data.Visitors == nil {
		r.data.Visitors = map[string]string{}
	}
	if r.data.Sites == nil {
		r.data.Sites = map[string]int64{}
	}
	if r.data.Since.IsZero() {
		r.data.Since = time.Now()
	}
	// A missing file previously rolled the day; keep behavior consistent when
	// the JSON itself was empty but the file existed.
	if r.data.Day == "" {
		r.data.Day = todayKey()
	}
	return nil
}

func (r *Recorder) save() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	r.data.UpdatedAt = time.Now()
	encoded, err := json.MarshalIndent(&r.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// Start persists dirty state every interval until Close is called.
func (r *Recorder) Start(interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	r.mu.Lock()
	done := make(chan struct{})
	r.done = done
	r.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.mu.Lock()
				dirty := r.dirty
				r.dirty = false
				r.mu.Unlock()
				if dirty {
					if err := r.save(); err != nil {
						r.mu.Lock()
						r.dirty = true // retry next tick
						r.mu.Unlock()
					}
				}
			case <-r.stopped:
				r.mu.Lock()
				r.dirty = false
				r.mu.Unlock()
				_ = r.save()
				return
			}
		}
	}()
}

// Close stops the background flusher and performs a final save. Safe to call
// on a recorder whose Start was never invoked.
func (r *Recorder) Close() {
	close(r.stopped)
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	<-done
}

// VisitorID returns a stable anonymous identifier for a client address.
func (r *Recorder) VisitorID(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	host = strings.ToLower(strings.TrimSpace(host))
	sum := sha256.Sum256([]byte(r.salt + "|" + host))
	return hex.EncodeToString(sum[:])
}

// Record marks activity from a visitor. site is the normalized website
// hostname (may be empty, e.g. for TCP tunnels). countUse selects whether the
// event counts as a full use; subresource requests still mark the visitor as
// active but do not inflate the usage totals or the site ranking.
func (r *Recorder) Record(visitorID, site string, countUse bool) {
	day := todayKey()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data.Visitors[visitorID] = day
	r.rollDayLocked(day)
	if countUse {
		r.data.UsesTotal++
		r.data.UsesToday++
		if site != "" {
			r.data.Sites[site]++
		}
	}
	r.dirty = true
}

// AddTraffic records transferred bytes (responses written to clients plus
// request bodies received). The daily traffic counter rolls over at the same
// local-day boundary as the usage counters.
func (r *Recorder) AddTraffic(bytes uint64) {
	if bytes == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rollDayLocked(todayKey())
	r.data.TrafficTotal += bytes
	r.data.TrafficToday += bytes
	r.dirty = true
}

func (r *Recorder) rollDayLocked(day string) {
	if r.data.Day != day {
		r.data.Day = day
		r.data.UsesToday = 0
		r.data.TrafficToday = 0
	}
}

// Snapshot returns a copy of the current statistics.
func (r *Recorder) Snapshot() Data {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyData := r.data
	copyData.Visitors = make(map[string]string, len(r.data.Visitors))
	for id, day := range r.data.Visitors {
		copyData.Visitors[id] = day
	}
	copyData.Sites = make(map[string]int64, len(r.data.Sites))
	for site, uses := range r.data.Sites {
		copyData.Sites[site] = uses
	}
	return copyData
}

// VisitorsToday counts visitors whose last activity fell on the current day.
func (r *Recorder) VisitorsToday(data Data) int64 {
	day := todayKey()
	var count int64
	for _, lastDay := range data.Visitors {
		if lastDay == day {
			count++
		}
	}
	return count
}

// NormalizeSite lowercases a hostname and trims trailing dots.
func NormalizeSite(hostname string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
}

func todayKey() string {
	return time.Now().Format("2006-01-02")
}
