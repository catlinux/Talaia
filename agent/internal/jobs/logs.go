package jobs

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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

// Logs returns the last lines of a container's stdout and stderr after since,
// as text. since zero means no lower limit; timestamps asks Docker for the
// timestamp prefix of each line.
func (d *DockerActions) Logs(ctx context.Context, container string, tail int, since time.Time, timestamps bool) (string, error) {
	if !ValidContainerRef(container) {
		return "", fmt.Errorf("invalid container reference")
	}

	query := url.Values{}
	query.Set("stdout", "1")
	query.Set("stderr", "1")
	query.Set("tail", strconv.Itoa(clampTail(tail)))
	// A core that predates the timestamps flag sends no since either: it still
	// gets the prefixes the agent has always added.
	if timestamps || since.IsZero() {
		query.Set("timestamps", "1")
	}
	if !since.IsZero() {
		query.Set("since", dockerSince(since))
	}
	path := fmt.Sprintf("/containers/%s/logs?%s", url.PathEscape(container), query.Encode())
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

// parseSince validates the optional RFC 3339 timestamp of a ReadLogs command.
// Nothing from the command reaches Docker without passing through here.
func parseSince(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("invalid since timestamp")
	}
	return parsed, nil
}

// dockerSince formats t as the Unix seconds and nanoseconds Docker accepts in
// its since parameter.
func dockerSince(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10) + "." + fmt.Sprintf("%09d", t.Nanosecond())
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
