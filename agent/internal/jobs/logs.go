package jobs

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	// maxLogBytes bounds the text a log read returns: the last bytes are kept.
	maxLogBytes = 256 << 10
	// maxRawLogBytes bounds how much of the Docker answer is read before the
	// bounded tail is cut.
	maxRawLogBytes = 8 << 20
	// defaultTail and maxTail clamp the number of lines asked for.
	defaultTail = 200
	maxTail     = 1000
)

// clampTail returns a usable number of lines.
func clampTail(tail int) int {
	switch {
	case tail <= 0:
		return defaultTail
	case tail > maxTail:
		return maxTail
	default:
		return tail
	}
}

// Logs returns the last lines of a container's stdout and stderr, with
// timestamps, as text.
func (d *DockerActions) Logs(ctx context.Context, container string, tail int) (string, error) {
	if !ValidContainerRef(container) {
		return "", fmt.Errorf("invalid container reference")
	}

	path := fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1&timestamps=1&tail=%d", url.PathEscape(container), clampTail(tail))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	response, err := d.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("docker is not reachable: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", dockerError(response)
	}

	raw, err := io.ReadAll(io.LimitReader(response.Body, maxRawLogBytes))
	if err != nil {
		return "", fmt.Errorf("read the logs: %w", err)
	}
	return lastBytes(demultiplex(raw), maxLogBytes), nil
}

// demultiplex removes the 8-byte frame headers Docker puts on the output of a
// container without a TTY. A container with a TTY sends plain text, which is
// returned as it is.
func demultiplex(raw []byte) string {
	if !looksFramed(raw) {
		return strings.ToValidUTF8(string(raw), "�")
	}

	var out bytes.Buffer
	for len(raw) >= 8 {
		size := int(binary.BigEndian.Uint32(raw[4:8]))
		raw = raw[8:]
		if size > len(raw) {
			size = len(raw)
		}
		out.Write(raw[:size])
		raw = raw[size:]
	}
	return strings.ToValidUTF8(out.String(), "�")
}

// looksFramed reports whether data starts with a Docker stream frame header: a
// stream id (0 stdin, 1 stdout, 2 stderr) and three zero bytes.
func looksFramed(data []byte) bool {
	return len(data) >= 8 && data[0] <= 2 && data[1] == 0 && data[2] == 0 && data[3] == 0
}

// lastBytes keeps at most max bytes from the end of text, without cutting a
// character in half and starting at a line boundary when it can.
func lastBytes(text string, max int) string {
	if len(text) <= max {
		return text
	}
	cut := text[len(text)-max:]
	for len(cut) > 0 && !utf8.RuneStart(cut[0]) {
		cut = cut[1:]
	}
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i < len(cut)-1 {
		cut = cut[i+1:]
	}
	return cut
}
