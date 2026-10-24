package api

import (
	"context"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

type AlertsStore interface {
	ListActiveAlerts(context.Context) ([]store.Alert, error)
	ListHistory(context.Context, int, int, *string) ([]store.Alert, error)
	GetAlert(context.Context, string) (store.Alert, error)
	AckAlert(context.Context, string, string, time.Time) error
	ResolveAlert(context.Context, string, time.Time) error
	ListActiveSilences(context.Context, time.Time) ([]store.Silence, error)
	CreateSilence(context.Context, store.Silence) (store.Silence, error)
	DeleteSilence(context.Context, string) error
	GetAllowPrivateTargets(context.Context) (bool, error)
	SetAllowPrivateTargets(context.Context, bool) error
	ListServers(context.Context) ([]store.Server, error)
	GetServer(context.Context, string) (store.Server, error)
	ListAlertRules(context.Context) ([]store.AlertRule, error)
	GetAlertRule(context.Context, string) (store.AlertRule, error)
	CreateAlertRule(context.Context, store.AlertRule) (store.AlertRule, error)
	UpdateAlertRule(context.Context, store.AlertRule) error
	DeleteAlertRule(context.Context, string) error
	GetGroup(context.Context, string) (store.ServerGroup, error)
	ListNotificationChannels(context.Context) ([]store.NotificationChannel, error)
	GetNotificationChannel(context.Context, string) (store.NotificationChannel, error)
	CreateNotificationChannel(context.Context, store.NotificationChannel, []byte) (store.NotificationChannel, error)
	UpdateNotificationChannel(context.Context, store.NotificationChannel, []byte) error
	DeleteNotificationChannel(context.Context, string) error
	GetChannelSecret(context.Context, string) ([]byte, error)
	ListAlertChecks(context.Context) ([]store.AlertCheck, error)
	GetAlertCheck(context.Context, string) (store.AlertCheck, error)
	CreateAlertCheck(context.Context, store.AlertCheck) (store.AlertCheck, error)
	UpdateAlertCheck(context.Context, store.AlertCheck) error
	DeleteAlertCheck(context.Context, string) error
	LatestAlertCheckResult(context.Context, string) (store.AlertCheckResult, error)
}

type CheckControl interface {
	Reload()
	RunOnce(context.Context, store.AlertCheck) store.AlertCheckResult
}
type ChannelTester interface {
	SendTest(context.Context, string) error
}
type AlertsDeps struct {
	Store    AlertsStore
	Box      *auth.SecretBox
	Audit    Auditor
	Hooks    AlertHooks
	Checks   CheckControl
	Notifier ChannelTester
	Servers  ServerLister
	Now      func() time.Time
}

func (d AlertsDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}
func alertTarget(a store.Alert, groupID string) rbac.Target {
	t := rbac.Target{}
	if a.ServerID != nil {
		t.ServerID = *a.ServerID
		t.GroupID = groupID
	}
	t.Stack = a.Stack
	if a.SubjectKind == "container" {
		t.Container = a.SubjectName
	}
	return t
}
