package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

// Recording failures. A session is closed with these reasons: a session that
// cannot be recorded must not go on.
const (
	reasonRecordingLimit  = "recording_limit"
	reasonRecordingFailed = "recording_failed"
)

// recorder keeps the output of a session as an asciicast v2 file, in chunks. Each
// chunk is sealed with the position it has in the recording, so a chunk cannot be
// moved, repeated or dropped without the export noticing. The keystrokes are not
// recorded.
type recorder struct {
	id         string
	seal       Sealer
	store      Store
	started    time.Time
	chunkBytes int
	limit      int64

	// flushMu serialises the flushes, so a chunk is never stored twice.
	flushMu sync.Mutex

	mu    sync.Mutex
	buf   bytes.Buffer
	carry []byte // the start of a character that the next output completes
	seq   int
	total int64
	// wake asks the writer to flush without waiting for its timer.
	wake chan struct{}
}

func newRecorder(id string, seal Sealer, store Store, chunkBytes int, limit int64, started time.Time, container, shell string, cols, rows uint32) *recorder {
	r := &recorder{
		id: id, seal: seal, store: store, started: started,
		chunkBytes: chunkBytes, limit: limit, wake: make(chan struct{}, 1),
	}
	header, _ := json.Marshal(map[string]any{
		"version":   2,
		"width":     cols,
		"height":    rows,
		"timestamp": started.Unix(),
		"title":     container,
		"env":       map[string]string{"SHELL": "/bin/" + shell, "TERM": "xterm-256color"},
	})
	r.buf.Write(header)
	r.buf.WriteByte('\n')
	r.total = int64(r.buf.Len())
	return r
}

// add records output. It returns "" or the reason the session must end.
func (r *recorder) add(data []byte, now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	text := append(append([]byte(nil), r.carry...), data...)
	complete, rest := splitIncomplete(text)
	r.carry = append([]byte(nil), rest...)
	if len(complete) == 0 {
		return ""
	}
	return r.event(now, "o", string(complete))
}

// resize records a change of the size of the terminal.
func (r *recorder) resize(cols, rows uint32, now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.event(now, "r", fmt.Sprintf("%dx%d", cols, rows))
}

// event appends one asciicast event. The caller holds the lock.
func (r *recorder) event(now time.Time, kind, data string) string {
	text, _ := json.Marshal(data) // invalid UTF-8 becomes U+FFFD
	seconds := strconv.FormatFloat(now.Sub(r.started).Seconds(), 'f', 6, 64)
	line := "[" + seconds + ", \"" + kind + "\", " + string(text) + "]\n"

	r.total += int64(len(line))
	if r.total > r.limit {
		return reasonRecordingLimit
	}
	// The writer keeps up unless the database is stuck: then the session ends.
	if r.buf.Len() > 4*r.chunkBytes+len(line) {
		return reasonRecordingFailed
	}
	r.buf.WriteString(line)
	if r.buf.Len() >= r.chunkBytes {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
	return ""
}

// flush seals and stores what is buffered as the next chunk.
func (r *recorder) flush(ctx context.Context) error {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()

	r.mu.Lock()
	if r.buf.Len() == 0 {
		r.mu.Unlock()
		return nil
	}
	plain := append([]byte(nil), r.buf.Bytes()...)
	seq := r.seq
	r.mu.Unlock()

	sealed := r.seal.SealWithContext(plain, chunkContext(r.id, seq))
	if err := r.store.AppendTerminalChunk(ctx, r.id, seq, sealed, len(plain)); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf.Next(len(plain))
	r.seq++
	return nil
}

// finish records the characters still pending and writes the last chunk.
func (r *recorder) finish(ctx context.Context, now time.Time) error {
	r.mu.Lock()
	if len(r.carry) > 0 {
		// The recording may exceed its limit by this last character: it is part
		// of what the person saw.
		r.event(now, "o", string(r.carry))
		r.carry = nil
	}
	r.mu.Unlock()
	return r.flush(ctx)
}

// run flushes every interval and whenever a chunk is full, until ctx ends.
// onError is called with the first failure.
func (r *recorder) run(ctx context.Context, interval time.Duration, onError func(error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
		if err := r.flush(ctx); err != nil {
			onError(err)
			return
		}
	}
}

// chunkContext is what a chunk is sealed against: the session and its position.
func chunkContext(id string, seq int) []byte {
	return []byte(id + "|" + strconv.Itoa(seq))
}

// splitIncomplete cuts the bytes of a character that is not complete yet off the
// end of data.
func splitIncomplete(data []byte) (complete, rest []byte) {
	for back := 1; back <= utf8.UTFMax-1 && back <= len(data); back++ {
		if utf8.RuneStart(data[len(data)-back]) {
			if !utf8.FullRune(data[len(data)-back:]) {
				return data[:len(data)-back], data[len(data)-back:]
			}
			break
		}
	}
	return data, nil
}
