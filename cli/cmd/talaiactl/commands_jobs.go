package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/catlinux/Talaia/cli/i18n"
)

func (a *app) jobsCommand(localizer *i18n.Localizer, opts options, args []string) int {
	if len(args) != 2 || args[0] != "wait" {
		return a.usageError(localizer)
	}
	client, code := a.prepare(localizer, opts)
	if code >= 0 {
		return code
	}
	return a.waitJob(localizer, client, opts, args[1])
}

// waitJob polls a job until it finishes or the timeout expires.
func (a *app) waitJob(localizer *i18n.Localizer, client *apiClient, opts options, jobID string) int {
	deadline := time.Now().Add(opts.timeout)
	path := "/api/v1/jobs/" + url.PathEscape(jobID)
	for {
		raw, err := client.get(context.Background(), path)
		if err != nil {
			return a.fail(localizer, err)
		}
		var current job
		if err := json.Unmarshal(raw, &current); err != nil {
			return a.fail(localizer, &invalidResponseError{err: err})
		}
		if current.State == "succeeded" || current.State == "failed" {
			if opts.json {
				if err := writeRawJSON(a.stdout, raw); err != nil {
					return a.fail(localizer, err)
				}
			} else {
				writeJobOutput(a.stdout, current.Output)
			}
			if current.State == "failed" {
				message := current.Message
				if strings.TrimSpace(message) == "" {
					message = localizer.T("job.noMessage", nil)
				}
				fmt.Fprintln(a.stderr, localizer.T("app.name", nil)+": "+localizer.T("job.failed", map[string]any{
					"JobID": current.ID, "Message": message,
				}))
				return 1
			}
			return 0
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			fmt.Fprintln(a.stderr, localizer.T("app.name", nil)+": "+localizer.T("job.timeout", map[string]any{
				"JobID": jobID,
			}))
			return 1
		}
		wait := a.pollInterval
		if wait <= 0 {
			wait = 2 * time.Second
		}
		if wait > remaining {
			wait = remaining
		}
		time.Sleep(wait)
	}
}

func writeJobOutput(out io.Writer, output string) {
	if output == "" {
		return
	}
	_, _ = out.Write([]byte(output))
	if !strings.HasSuffix(output, "\n") {
		_, _ = out.Write([]byte("\n"))
	}
}
