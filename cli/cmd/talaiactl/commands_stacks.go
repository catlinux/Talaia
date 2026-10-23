package main

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/catlinux/Talaia/cli/i18n"
)

func (a *app) stacksCommand(localizer *i18n.Localizer, opts options, args []string) int {
	if len(args) < 1 {
		return a.usageError(localizer)
	}
	switch args[0] {
	case "list":
		if len(args) > 2 {
			return a.usageError(localizer)
		}
	case "up", "down", "restart", "pull":
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
		if len(args) == 2 {
			return a.serverStacks(localizer, client, opts, args[1])
		}
		return a.allStacks(localizer, client, opts)
	default:
		return a.stackAction(localizer, client, opts, args[0], args[1], args[2])
	}
}

func (a *app) allStacks(localizer *i18n.Localizer, client *apiClient, opts options) int {
	raw, err := client.get(context.Background(), "/api/v1/stacks")
	if err != nil {
		return a.fail(localizer, err)
	}
	if opts.json {
		if err := writeRawJSON(a.stdout, raw); err != nil {
			return a.fail(localizer, err)
		}
		return 0
	}
	var response allStacksResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return a.fail(localizer, &invalidResponseError{err: err})
	}
	rows := make([][]string, 0, len(response.Stacks))
	for _, stack := range response.Stacks {
		rows = append(rows, []string{stack.ServerName, stack.Name, stack.stateText(localizer)})
	}
	if err := writeTable(a.stdout, []string{
		localizer.T("table.server", nil),
		localizer.T("table.stack", nil),
		localizer.T("table.state", nil),
	}, rows); err != nil {
		return a.fail(localizer, err)
	}
	return 0
}

func (a *app) serverStacks(
	localizer *i18n.Localizer,
	client *apiClient,
	opts options,
	reference string,
) int {
	serverID, err := client.resolveServer(context.Background(), reference)
	if err != nil {
		return a.fail(localizer, err)
	}
	raw, err := client.get(context.Background(),
		"/api/v1/servers/"+url.PathEscape(serverID)+"/stacks")
	if err != nil {
		return a.fail(localizer, err)
	}
	if opts.json {
		if err := writeRawJSON(a.stdout, raw); err != nil {
			return a.fail(localizer, err)
		}
		return 0
	}
	var response serverStacksResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return a.fail(localizer, &invalidResponseError{err: err})
	}
	rows := make([][]string, 0,
		len(response.Stacks)+len(response.Adopted)+len(response.Broken)+len(response.Adoptable))
	for _, name := range response.Stacks {
		rows = append(rows, []string{name, localizer.T("state.managed", nil)})
	}
	for _, name := range response.Adopted {
		rows = append(rows, []string{name, localizer.T("state.adopted", nil)})
	}
	for _, name := range response.Broken {
		rows = append(rows, []string{name, localizer.T("state.broken", nil)})
	}
	for _, name := range response.Adoptable {
		rows = append(rows, []string{name, localizer.T("state.adoptable", nil)})
	}
	if err := writeTable(a.stdout, []string{
		localizer.T("table.stack", nil),
		localizer.T("table.state", nil),
	}, rows); err != nil {
		return a.fail(localizer, err)
	}
	return 0
}

func (a *app) stackAction(
	localizer *i18n.Localizer,
	client *apiClient,
	opts options,
	action, reference, stack string,
) int {
	serverID, err := client.resolveServer(context.Background(), reference)
	if err != nil {
		return a.fail(localizer, err)
	}
	body := map[string]any{"action": action}
	if action == "down" {
		body["confirm"] = true
	}
	raw, err := client.post(context.Background(),
		"/api/v1/servers/"+url.PathEscape(serverID)+"/stacks/"+url.PathEscape(stack)+"/actions",
		body)
	if err != nil {
		return a.fail(localizer, err)
	}
	return a.finishAction(localizer, client, opts, raw)
}

// stateText translates the state of an entry of GET /api/v1/stacks.
func (e stackEntry) stateText(localizer *i18n.Localizer) string {
	switch e.State {
	case "managed":
		return localizer.T("state.managed", nil)
	case "adopted":
		return localizer.T("state.adopted", nil)
	case "broken":
		return localizer.T("state.broken", nil)
	default:
		return e.State
	}
}
