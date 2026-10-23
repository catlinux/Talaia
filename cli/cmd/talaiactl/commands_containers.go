package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/catlinux/Talaia/cli/i18n"
)

func (a *app) containersCommand(localizer *i18n.Localizer, opts options, args []string) int {
	if len(args) < 2 {
		return a.usageError(localizer)
	}
	switch args[0] {
	case "list":
		if len(args) != 2 {
			return a.usageError(localizer)
		}
	case "start", "stop", "restart":
		if len(args) != 3 {
			return a.usageError(localizer)
		}
	default:
		return a.usageError(localizer)
	}
	client, code := a.prepare(localizer, opts)
	if code >= 0 {
		return code
	}
	switch args[0] {
	case "list":
		return a.containersList(localizer, client, opts, args[1])
	default:
		return a.containerAction(localizer, client, opts, args[0], args[1], args[2])
	}
}

func (a *app) containersList(localizer *i18n.Localizer, client *apiClient, opts options, reference string) int {
	serverID, err := client.resolveServer(context.Background(), reference)
	if err != nil {
		return a.fail(localizer, err)
	}
	raw, err := client.get(context.Background(),
		"/api/v1/servers/"+url.PathEscape(serverID)+"/containers")
	if err != nil {
		return a.fail(localizer, err)
	}
	if opts.json {
		if err := writeRawJSON(a.stdout, raw); err != nil {
			return a.fail(localizer, err)
		}
		return 0
	}
	var response containersResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return a.fail(localizer, &invalidResponseError{err: err})
	}
	rows := make([][]string, 0, len(response.Containers))
	for _, container := range response.Containers {
		rows = append(rows, []string{
			container.ID,
			container.Name,
			container.Image,
			container.State,
			container.Status,
		})
	}
	if err := writeTable(a.stdout, []string{
		localizer.T("table.id", nil),
		localizer.T("table.name", nil),
		localizer.T("table.image", nil),
		localizer.T("table.state", nil),
		localizer.T("table.status", nil),
	}, rows); err != nil {
		return a.fail(localizer, err)
	}
	return 0
}

func (a *app) containerAction(
	localizer *i18n.Localizer,
	client *apiClient,
	opts options,
	action, reference, container string,
) int {
	serverID, err := client.resolveServer(context.Background(), reference)
	if err != nil {
		return a.fail(localizer, err)
	}
	raw, err := client.post(context.Background(),
		"/api/v1/servers/"+url.PathEscape(serverID)+"/containers/"+
			url.PathEscape(container)+"/actions",
		map[string]any{"action": action})
	if err != nil {
		return a.fail(localizer, err)
	}
	return a.finishAction(localizer, client, opts, raw)
}

// finishAction handles the answer of every action that creates a job.
func (a *app) finishAction(localizer *i18n.Localizer, client *apiClient, opts options, raw []byte) int {
	var created createdJob
	if err := json.Unmarshal(raw, &created); err != nil {
		return a.fail(localizer, &invalidResponseError{err: err})
	}
	if created.JobID == "" {
		return a.fail(localizer, &invalidResponseError{err: fmt.Errorf("empty job id")})
	}
	if !opts.wait {
		if opts.json {
			if err := writeRawJSON(a.stdout, raw); err != nil {
				return a.fail(localizer, err)
			}
		} else {
			fmt.Fprintln(a.stdout, localizer.T("action.queued", map[string]any{"JobID": created.JobID}))
		}
		return 0
	}
	if !opts.json {
		fmt.Fprintln(a.stdout, localizer.T("action.queued", map[string]any{"JobID": created.JobID}))
	}
	return a.waitJob(localizer, client, opts, created.JobID)
}
