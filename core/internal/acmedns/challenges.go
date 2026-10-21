// Package acmedns answers the DNS-01 challenges of the panel certificate. The
// zone of the domain delegates _acme-challenge.<host> to this server with an
// NS record, so no DNS provider credentials ever reach the core.
//
// The core and the DNS server run as separate processes: the core writes the
// pending challenge values to a small file and the server only reads it.
package acmedns

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	// FileName is the challenge file inside the shared directory.
	FileName = "challenges.json"

	// maxFileBytes bounds what the server reads: a handful of records fit in a
	// few hundred bytes.
	maxFileBytes = 64 << 10
	// maxRecords bounds the records kept, so a misbehaving writer cannot grow
	// the answers.
	maxRecords = 32
	// maxValueLength is a base64url SHA-256, 43 characters; anything longer is
	// not a DNS-01 value.
	maxValueLength = 64
	// DefaultLifetime is how long a value is served without being removed: an
	// issuance that crashed half way must not leave it published for ever.
	DefaultLifetime = 30 * time.Minute

	fileMode = 0o644
)

// Record is one TXT value of one challenge name.
type Record struct {
	// Name is the fully qualified, lower case challenge name, with the final dot.
	Name    string    `json:"name"`
	Value   string    `json:"value"`
	Expires time.Time `json:"expires"`
}

type fileFormat struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

// CanonicalName returns name lower case and fully qualified.
func CanonicalName(name string) string {
	return dns.Fqdn(strings.ToLower(strings.TrimSpace(name)))
}

// ChallengeName returns the DNS-01 name of a host: _acme-challenge.<host>.
func ChallengeName(host string) string {
	return CanonicalName("_acme-challenge." + strings.TrimSuffix(strings.TrimSpace(host), "."))
}

func validValue(value string) bool {
	if value == "" || len(value) > maxValueLength {
		return false
	}
	for _, r := range value {
		if !isBase64URL(r) {
			return false
		}
	}
	return true
}

// isBase64URL reports whether r belongs to the base64url alphabet DNS-01
// values use.
func isBase64URL(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
}

// Writer keeps the challenge file of the core. It is the only writer.
type Writer struct {
	path string
	now  func() time.Time

	mu sync.Mutex
}

// NewWriter returns the writer of the challenge file in dir.
func NewWriter(dir string) *Writer {
	return &Writer{path: filepath.Join(dir, FileName), now: time.Now}
}

// Add publishes value under name until lifetime passes.
func (w *Writer) Add(name, value string, lifetime time.Duration) error {
	if !validValue(value) {
		return errors.New("invalid challenge value")
	}
	name = CanonicalName(name)
	if _, ok := dns.IsDomainName(name); !ok || strings.Contains(name, "..") {
		return fmt.Errorf("invalid challenge name %q", name)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	records := w.live()
	for _, record := range records {
		if record.Name == name && record.Value == value {
			return w.write(records)
		}
	}
	if len(records) >= maxRecords {
		return errors.New("too many pending challenges")
	}
	records = append(records, Record{Name: name, Value: value, Expires: w.now().Add(lifetime).UTC()})
	return w.write(records)
}

// Remove withdraws value from name. Removing what is not there is not an error.
func (w *Writer) Remove(name, value string) error {
	name = CanonicalName(name)

	w.mu.Lock()
	defer w.mu.Unlock()

	records := w.live()
	kept := records[:0]
	for _, record := range records {
		if record.Name != name || record.Value != value {
			kept = append(kept, record)
		}
	}
	return w.write(kept)
}

// live reads the current records and drops the expired and the malformed ones.
// A missing or unreadable file counts as empty: the core rewrites it whole.
func (w *Writer) live() []Record {
	records, err := readFile(w.path)
	if err != nil {
		return nil
	}
	return filterLive(records, w.now())
}

func (w *Writer) write(records []Record) error {
	if records == nil {
		records = []Record{}
	}
	data, err := json.Marshal(fileFormat{Version: 1, Records: records})
	if err != nil {
		return err
	}
	dir := filepath.Dir(w.path)
	tmp, err := os.CreateTemp(dir, ".challenges-*")
	if err != nil {
		return fmt.Errorf("create the challenge file: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	// The DNS server runs as another process and only reads: the values are
	// public by nature, they end up in public DNS answers.
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, w.path); err != nil {
		return fmt.Errorf("publish the challenge file: %w", err)
	}
	return nil
}

func readFile(path string) ([]Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, errors.New("the challenge file is too large")
	}
	var parsed fileFormat
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse the challenge file: %w", err)
	}
	return parsed.Records, nil
}

func filterLive(records []Record, now time.Time) []Record {
	live := make([]Record, 0, len(records))
	for _, record := range records {
		if len(live) >= maxRecords {
			break
		}
		if !validValue(record.Value) || !now.Before(record.Expires) {
			continue
		}
		record.Name = CanonicalName(record.Name)
		live = append(live, record)
	}
	return live
}

// Reader serves the records of the challenge file to the DNS server. It reads
// the file again at most once per interval, so a flood of queries does not
// turn into a flood of reads.
type Reader struct {
	path     string
	interval time.Duration
	now      func() time.Time

	mu       sync.Mutex
	checked  time.Time
	modified time.Time
	records  []Record
}

// NewReader returns a reader of the challenge file in dir.
func NewReader(dir string) *Reader {
	return &Reader{path: filepath.Join(dir, FileName), interval: 500 * time.Millisecond, now: time.Now}
}

// Values returns the live TXT values of name and the time the file last
// changed, which the SOA serial follows.
func (r *Reader) Values(name string) ([]string, time.Time) {
	name = CanonicalName(name)
	now := r.now()

	r.mu.Lock()
	defer r.mu.Unlock()

	if now.Sub(r.checked) >= r.interval || now.Before(r.checked) {
		r.checked = now
		r.reload()
	}

	var values []string
	for _, record := range r.records {
		if record.Name == name && now.Before(record.Expires) {
			values = append(values, record.Value)
		}
	}
	return values, r.modified
}

func (r *Reader) reload() {
	info, err := os.Stat(r.path)
	if err != nil {
		// No file means no pending challenge.
		r.records, r.modified = nil, time.Time{}
		return
	}
	if info.ModTime().Equal(r.modified) && r.records != nil {
		return
	}
	records, err := readFile(r.path)
	if err != nil {
		// Keep serving what was valid: a half written file is never seen
		// (the writer renames), so this is a broken file, not a race.
		return
	}
	r.records = filterLive(records, r.now())
	r.modified = info.ModTime()
}
