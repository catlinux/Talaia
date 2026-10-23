package main

import (
	"context"
	"encoding/json"

	"github.com/catlinux/Talaia/cli/i18n"
)

func (a *app) serversCommand(localizer *i18n.Localizer, opts options, args []string) int {
	if len(args) != 1 || args[0] != "list" {
		return a.usageError(localizer)
	}
	client, code := a.prepare(localizer, opts)
	if code >= 0 {
		return code
	}
	raw, err := client.get(context.Background(), "/api/v1/servers")
	if err != nil {
		return a.fail(localizer, err)
	}
	if opts.json {
		if err := writeRawJSON(a.stdout, raw); err != nil {
			return a.fail(localizer, err)
		}
		return 0
	}
	var response serversResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return a.fail(localizer, &invalidResponseError{err: err})
	}
	rows := make([][]string, 0, len(response.Servers))
	for _, server := range response.Servers {
		agent := "-"
		if server.Live != nil && server.Live.AgentVersion != "" {
			agent = server.Live.AgentVersion
		}
		rows = append(rows, []string{
			server.ID,
			server.Name,
			a.serverStatus(localizer, server),
			agent,
		})
	}
	if err := writeTable(a.stdout, []string{
		localizer.T("table.id", nil),
		localizer.T("table.name", nil),
		localizer.T("table.status", nil),
		localizer.T("table.agent", nil),
	}, rows); err != nil {
		return a.fail(localizer, err)
	}
	return 0
}

func (a *app) serverStatus(localizer *i18n.Localizer, server server) string {
	if server.Revoked {
		return localizer.T("status.revoked", nil)
	}
	if server.Limited {
		if server.Status == "online" {
			return localizer.T("status.online", nil)
		}
		return localizer.T("status.offline", nil)
	}
	if server.Connected {
		return localizer.T("status.online", nil)
	}
	return localizer.T("status.offline", nil)
}
