package terminal

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

// Opener decrypts the chunks of a recording.
type Opener interface {
	OpenWithContext(sealed, bound []byte) ([]byte, error)
}

// Recordings is the part of the store that holds recordings.
type Recordings interface {
	GetTerminalSession(ctx context.Context, id string) (store.TerminalSession, error)
	TerminalChunks(ctx context.Context, id string) ([]store.TerminalChunk, error)
}

// Export writes the recording of a session to w as an asciicast v2 file. It
// refuses a recording with a chunk missing, repeated, moved or altered.
func Export(ctx context.Context, recordings Recordings, opener Opener, id string, w io.Writer) error {
	if _, err := recordings.GetTerminalSession(ctx, id); err != nil {
		return err
	}
	chunks, err := recordings.TerminalChunks(ctx, id)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return fmt.Errorf("the session %s has no recording", id)
	}

	for position, chunk := range chunks {
		if chunk.Seq != position {
			return fmt.Errorf("the recording is incomplete: chunk %d is missing", position)
		}
		plain, err := opener.OpenWithContext(chunk.Sealed, chunkContext(id, chunk.Seq))
		if err != nil {
			return fmt.Errorf("chunk %d of the recording cannot be opened: %w", chunk.Seq, err)
		}
		if _, err := w.Write(plain); err != nil {
			return err
		}
	}
	return nil
}

// Purger deletes old recordings.
type Purger interface {
	PurgeTerminalSessions(ctx context.Context, cutoff time.Time) (int64, error)
}

// PurgeLoop deletes the closed sessions older than days now and then every
// interval, until ctx ends. days of 0 or less keeps everything.
func PurgeLoop(ctx context.Context, purger Purger, days int, interval time.Duration) {
	if days <= 0 {
		return
	}
	purge := func() {
		cutoff := time.Now().AddDate(0, 0, -days)
		deleted, err := purger.PurgeTerminalSessions(ctx, cutoff)
		if err != nil {
			slog.Error("purge the terminal recordings", "error", err)
			return
		}
		if deleted > 0 {
			slog.Info("purged old terminal recordings", "count", deleted, "older_than_days", days)
		}
	}

	purge()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}
