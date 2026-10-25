package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// AlertRule is a configurable alert definition.
type AlertRule struct {
	ID, Name, Type, Severity               string
	Enabled, Builtin                       bool
	Params                                 json.RawMessage
	ScopeType                              string
	ScopeServerID, ScopeGroupID, ScopeName *string
	ChannelIDs                             []string
	RepeatIntervalSeconds                  int
	CreatedAt, UpdatedAt                   time.Time
	UpdatedBy                              string
}

// Alert is an incident raised by an alert rule.
type Alert struct {
	ID                                     string
	RuleID                                 *string
	RuleType, Severity, Fingerprint        string
	ServerID                               *string
	SubjectKind, SubjectName, Stack, State string
	Summary                                json.RawMessage
	StartedAt                              time.Time
	ResolvedAt, AckedAt, LastNotifiedAt    *time.Time
	AckedBy                                string
	Silenced                               bool
}

// AlertWatch tracks when a container was last observed running.
type AlertWatch struct {
	ServerID, ContainerName string
	LastRunningAt           time.Time
}

// Silence suppresses matching alerts until EndsAt.
type Silence struct {
	ID, ScopeType                          string
	ScopeServerID, ScopeGroupID, ScopeName *string
	RuleType                               *string
	Reason, CreatedBy                      string
	CreatedAt, EndsAt                      time.Time
}

// AlertCheck is an HTTP, TCP or TLS check definition.
type AlertCheck struct {
	ID, Name, Kind, Target string
	Config                 json.RawMessage
	IntervalSeconds        int
	Enabled                bool
	ServerID               *string
	CreatedAt, UpdatedAt   time.Time
	UpdatedBy              string
}

// AlertCheckResult is one check observation.
type AlertCheckResult struct {
	Time         time.Time
	CheckID      string
	OK           bool
	LatencyMS    int
	StatusCode   *int
	Error        string
	CertNotAfter *time.Time
}

// NotificationChannel exposes no secret bytes. Use GetChannelSecret for the
// sealed payload required by a notifier.
type NotificationChannel struct {
	ID, Name, Type       string
	Enabled              bool
	Language             string
	Config               json.RawMessage
	HasSecret            bool
	LastError            string
	LastOKAt             *time.Time
	CreatedAt, UpdatedAt time.Time
	UpdatedBy            string
}

// NotificationDelivery is a queued channel delivery.
type NotificationDelivery struct {
	ID                 string
	AlertID, ChannelID *string
	Event              string
	Attempts           int
	NextAttemptAt      time.Time
	Status, LastError  string
	CreatedAt          time.Time
	SentAt             *time.Time
}

const alertRuleColumns = `id,name,type,enabled,severity,params,scope_type,scope_server_id,scope_group_id,scope_name,channel_ids,repeat_interval_seconds,builtin,created_at,updated_at,updated_by`
const alertColumns = `id,rule_id,rule_type,severity,fingerprint,server_id,subject_kind,subject_name,stack,state,summary,started_at,resolved_at,acked_at,acked_by,last_notified_at,silenced`
const silenceColumns = `id,scope_type,scope_server_id,scope_group_id,scope_name,rule_type,reason,created_by,created_at,ends_at`
const checkColumns = `id,name,kind,target,config,interval_seconds,enabled,server_id,created_at,updated_at,updated_by`
const channelColumns = `id,name,type,enabled,language,config,(secret_enc IS NOT NULL),last_error,last_ok_at,created_at,updated_at,updated_by`
const deliveryColumns = `id,alert_id,channel_id,event,attempts,next_attempt_at,status,last_error,created_at,sent_at`

func scanAlertRule(row pgx.Row) (v AlertRule, err error) {
	err = row.Scan(&v.ID, &v.Name, &v.Type, &v.Enabled, &v.Severity, &v.Params, &v.ScopeType, &v.ScopeServerID, &v.ScopeGroupID, &v.ScopeName, &v.ChannelIDs, &v.RepeatIntervalSeconds, &v.Builtin, &v.CreatedAt, &v.UpdatedAt, &v.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return
}
func scanAlert(row pgx.Row) (v Alert, err error) {
	err = row.Scan(&v.ID, &v.RuleID, &v.RuleType, &v.Severity, &v.Fingerprint, &v.ServerID, &v.SubjectKind, &v.SubjectName, &v.Stack, &v.State, &v.Summary, &v.StartedAt, &v.ResolvedAt, &v.AckedAt, &v.AckedBy, &v.LastNotifiedAt, &v.Silenced)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return
}
func scanSilence(row pgx.Row) (v Silence, err error) {
	err = row.Scan(&v.ID, &v.ScopeType, &v.ScopeServerID, &v.ScopeGroupID, &v.ScopeName, &v.RuleType, &v.Reason, &v.CreatedBy, &v.CreatedAt, &v.EndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return
}
func scanAlertCheck(row pgx.Row) (v AlertCheck, err error) {
	err = row.Scan(&v.ID, &v.Name, &v.Kind, &v.Target, &v.Config, &v.IntervalSeconds, &v.Enabled, &v.ServerID, &v.CreatedAt, &v.UpdatedAt, &v.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return
}
func scanChannel(row pgx.Row) (v NotificationChannel, err error) {
	err = row.Scan(&v.ID, &v.Name, &v.Type, &v.Enabled, &v.Language, &v.Config, &v.HasSecret, &v.LastError, &v.LastOKAt, &v.CreatedAt, &v.UpdatedAt, &v.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return
}
func scanDelivery(row pgx.Row) (v NotificationDelivery, err error) {
	err = row.Scan(&v.ID, &v.AlertID, &v.ChannelID, &v.Event, &v.Attempts, &v.NextAttemptAt, &v.Status, &v.LastError, &v.CreatedAt, &v.SentAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return
}

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+alertRuleColumns+` FROM alert_rule ORDER BY name,id`)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		v, e := scanAlertRule(rows)
		if e != nil {
			return nil, fmt.Errorf("scan alert rule: %w", e)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) GetAlertRule(ctx context.Context, id string) (AlertRule, error) {
	v, e := scanAlertRule(s.pool.QueryRow(ctx, `SELECT `+alertRuleColumns+` FROM alert_rule WHERE id=$1`, id))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return v, fmt.Errorf("get alert rule: %w", e)
	}
	return v, e
}
func (s *Store) CreateAlertRule(ctx context.Context, v AlertRule) (AlertRule, error) {
	if len(v.Params) == 0 {
		v.Params = json.RawMessage(`{}`)
	}
	if v.ChannelIDs == nil {
		v.ChannelIDs = []string{}
	}
	out, e := scanAlertRule(s.pool.QueryRow(ctx, `INSERT INTO alert_rule(name,type,enabled,severity,params,scope_type,scope_server_id,scope_group_id,scope_name,channel_ids,repeat_interval_seconds,builtin,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+alertRuleColumns, v.Name, v.Type, v.Enabled, v.Severity, v.Params, v.ScopeType, v.ScopeServerID, v.ScopeGroupID, v.ScopeName, v.ChannelIDs, v.RepeatIntervalSeconds, v.Builtin, v.UpdatedBy))
	if e != nil {
		return out, fmt.Errorf("create alert rule: %w", e)
	}
	return out, nil
}
func (s *Store) UpdateAlertRule(ctx context.Context, v AlertRule) error {
	tag, e := s.pool.Exec(ctx, `UPDATE alert_rule SET name=$2,type=$3,enabled=$4,severity=$5,params=$6,scope_type=$7,scope_server_id=$8,scope_group_id=$9,scope_name=$10,channel_ids=$11,repeat_interval_seconds=$12,updated_at=now(),updated_by=$13 WHERE id=$1`, v.ID, v.Name, v.Type, v.Enabled, v.Severity, v.Params, v.ScopeType, v.ScopeServerID, v.ScopeGroupID, v.ScopeName, v.ChannelIDs, v.RepeatIntervalSeconds, v.UpdatedBy)
	if e != nil {
		return fmt.Errorf("update alert rule: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DeleteAlertRule(ctx context.Context, id string) error {
	tag, e := s.pool.Exec(ctx, `DELETE FROM alert_rule WHERE id=$1 AND NOT builtin`, id)
	if e != nil {
		return fmt.Errorf("delete alert rule: %w", e)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if e = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alert_rule WHERE id=$1)`, id).Scan(&exists); e != nil {
			return fmt.Errorf("check alert rule: %w", e)
		}
		if !exists {
			return ErrNotFound
		}
		return fmt.Errorf("builtin alert rule cannot be deleted")
	}
	return nil
}

// OpenAlert inserts a firing incident unless its fingerprint is already open.
func (s *Store) OpenAlert(ctx context.Context, v Alert) (Alert, bool, error) {
	if len(v.Summary) == 0 {
		v.Summary = json.RawMessage(`{}`)
	}
	q := `INSERT INTO alert(rule_id,rule_type,severity,fingerprint,server_id,subject_kind,subject_name,stack,state,summary,started_at,silenced) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'firing',COALESCE($9,'{}'::jsonb),COALESCE($10,now()),$11) ON CONFLICT (fingerprint) WHERE state='firing' DO NOTHING RETURNING ` + alertColumns
	out, e := scanAlert(s.pool.QueryRow(ctx, q, v.RuleID, v.RuleType, v.Severity, v.Fingerprint, v.ServerID, v.SubjectKind, v.SubjectName, v.Stack, v.Summary, v.StartedAt, v.Silenced))
	if e == nil {
		return out, true, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return out, false, fmt.Errorf("open alert: %w", e)
	}
	out, e = scanAlert(s.pool.QueryRow(ctx, `SELECT `+alertColumns+` FROM alert WHERE fingerprint=$1 AND state='firing'`, v.Fingerprint))
	if e != nil {
		return out, false, fmt.Errorf("get open alert: %w", e)
	}
	return out, false, nil
}
func (s *Store) ResolveAlert(ctx context.Context, id string, at time.Time) error {
	tag, e := s.pool.Exec(ctx, `UPDATE alert SET state='resolved',resolved_at=$2 WHERE id=$1 AND state='firing'`, id, at)
	if e != nil {
		return fmt.Errorf("resolve alert: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) AckAlert(ctx context.Context, id, by string, at time.Time) error {
	tag, e := s.pool.Exec(ctx, `UPDATE alert SET acked_at=$2,acked_by=$3 WHERE id=$1`, id, at, by)
	if e != nil {
		return fmt.Errorf("ack alert: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAlertSilenced records whether an open incident is currently covered by a
// silence, so the panel shows it even when the silence began after it fired.
func (s *Store) SetAlertSilenced(ctx context.Context, id string, silenced bool) error {
	tag, e := s.pool.Exec(ctx, `UPDATE alert SET silenced=$2 WHERE id=$1`, id, silenced)
	if e != nil {
		return fmt.Errorf("set alert silenced: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListActiveAlerts(ctx context.Context) ([]Alert, error) {
	return s.listAlerts(ctx, `SELECT `+alertColumns+` FROM alert WHERE state='firing' ORDER BY started_at DESC`, nil)
}
func (s *Store) ListHistory(ctx context.Context, limit, offset int, serverID *string) ([]Alert, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	return s.listAlerts(ctx, `SELECT `+alertColumns+` FROM alert WHERE ($3::uuid IS NULL OR server_id=$3) ORDER BY started_at DESC LIMIT $1 OFFSET $2`, []any{limit, offset, serverID})
}
func (s *Store) listAlerts(ctx context.Context, q string, args []any) ([]Alert, error) {
	var rows pgx.Rows
	var e error
	if args == nil {
		rows, e = s.pool.Query(ctx, q)
	} else {
		rows, e = s.pool.Query(ctx, q, args...)
	}
	if e != nil {
		return nil, fmt.Errorf("list alerts: %w", e)
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		v, se := scanAlert(rows)
		if se != nil {
			return nil, fmt.Errorf("scan alert: %w", se)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// PurgeAlertHistory deletes the resolved incidents resolved before alertsBefore
// and the delivery rows already sent or given up before deliveriesBefore. Open
// incidents and deliveries still waiting for a retry are never touched.
func (s *Store) PurgeAlertHistory(ctx context.Context, alertsBefore, deliveriesBefore time.Time) (alerts, deliveries int64, err error) {
	tag, e := s.pool.Exec(ctx, `DELETE FROM notification_delivery WHERE status IN ('sent','failed') AND created_at < $1`, deliveriesBefore)
	if e != nil {
		return 0, 0, fmt.Errorf("purge notification deliveries: %w", e)
	}
	deliveries = tag.RowsAffected()
	tag, e = s.pool.Exec(ctx, `DELETE FROM alert WHERE state='resolved' AND resolved_at < $1`, alertsBefore)
	if e != nil {
		return 0, deliveries, fmt.Errorf("purge resolved alerts: %w", e)
	}
	return tag.RowsAffected(), deliveries, nil
}

// GetAlert returns one incident by id.
func (s *Store) GetAlert(ctx context.Context, id string) (Alert, error) {
	v, e := scanAlert(s.pool.QueryRow(ctx, `SELECT `+alertColumns+` FROM alert WHERE id=$1`, id))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return v, fmt.Errorf("get alert: %w", e)
	}
	return v, e
}

// SetChannelStatus records the outcome of the last delivery attempt of a
// channel: a success clears last_error and sets last_ok_at, a failure keeps the
// (already redacted) message.
func (s *Store) SetChannelStatus(ctx context.Context, id string, ok bool, message string, at time.Time) error {
	var tag pgconn.CommandTag
	var e error
	if ok {
		tag, e = s.pool.Exec(ctx, `UPDATE notification_channel SET last_error='',last_ok_at=$2 WHERE id=$1`, id, at)
	} else {
		tag, e = s.pool.Exec(ctx, `UPDATE notification_channel SET last_error=$2 WHERE id=$1`, id, message)
	}
	if e != nil {
		return fmt.Errorf("set channel status: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetAlertByFingerprint(ctx context.Context, fp string) (Alert, error) {
	v, e := scanAlert(s.pool.QueryRow(ctx, `SELECT `+alertColumns+` FROM alert WHERE fingerprint=$1 ORDER BY started_at DESC LIMIT 1`, fp))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return v, fmt.Errorf("get alert by fingerprint: %w", e)
	}
	return v, e
}
func (s *Store) MarkAlertNotified(ctx context.Context, id string, at time.Time) error {
	tag, e := s.pool.Exec(ctx, `UPDATE alert SET last_notified_at=$2 WHERE id=$1`, id, at)
	if e != nil {
		return fmt.Errorf("mark alert notified: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpsertWatch(ctx context.Context, v AlertWatch) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO alert_watch(server_id,container_name,last_running_at) VALUES($1,$2,$3) ON CONFLICT(server_id,container_name) DO UPDATE SET last_running_at=EXCLUDED.last_running_at`, v.ServerID, v.ContainerName, v.LastRunningAt)
	if e != nil {
		return fmt.Errorf("upsert alert watch: %w", e)
	}
	return nil
}
func (s *Store) DeleteWatch(ctx context.Context, serverID, name string) error {
	tag, e := s.pool.Exec(ctx, `DELETE FROM alert_watch WHERE server_id=$1 AND container_name=$2`, serverID, name)
	if e != nil {
		return fmt.Errorf("delete alert watch: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) ListWatches(ctx context.Context) ([]AlertWatch, error) {
	rows, e := s.pool.Query(ctx, `SELECT server_id,container_name,last_running_at FROM alert_watch ORDER BY server_id,container_name`)
	if e != nil {
		return nil, fmt.Errorf("list alert watches: %w", e)
	}
	defer rows.Close()
	out := []AlertWatch{}
	for rows.Next() {
		var v AlertWatch
		if e = rows.Scan(&v.ServerID, &v.ContainerName, &v.LastRunningAt); e != nil {
			return nil, fmt.Errorf("scan alert watch: %w", e)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) CreateSilence(ctx context.Context, v Silence) (Silence, error) {
	if !v.EndsAt.After(time.Now()) || v.EndsAt.After(time.Now().Add(30*24*time.Hour)) {
		return Silence{}, fmt.Errorf("silence end must be within 30 days")
	}
	out, e := scanSilence(s.pool.QueryRow(ctx, `INSERT INTO silence(scope_type,scope_server_id,scope_group_id,scope_name,rule_type,reason,created_by,ends_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+silenceColumns, v.ScopeType, v.ScopeServerID, v.ScopeGroupID, v.ScopeName, v.RuleType, v.Reason, v.CreatedBy, v.EndsAt))
	if e != nil {
		return out, fmt.Errorf("create silence: %w", e)
	}
	return out, nil
}
func (s *Store) ListActiveSilences(ctx context.Context, now time.Time) ([]Silence, error) {
	rows, e := s.pool.Query(ctx, `SELECT `+silenceColumns+` FROM silence WHERE ends_at>$1 ORDER BY ends_at`, now)
	if e != nil {
		return nil, fmt.Errorf("list silences: %w", e)
	}
	defer rows.Close()
	out := []Silence{}
	for rows.Next() {
		v, se := scanSilence(rows)
		if se != nil {
			return nil, fmt.Errorf("scan silence: %w", se)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) DeleteSilence(ctx context.Context, id string) error {
	tag, e := s.pool.Exec(ctx, `DELETE FROM silence WHERE id=$1`, id)
	if e != nil {
		return fmt.Errorf("delete silence: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListAlertChecks(ctx context.Context) ([]AlertCheck, error) {
	rows, e := s.pool.Query(ctx, `SELECT `+checkColumns+` FROM alert_check ORDER BY name,id`)
	if e != nil {
		return nil, fmt.Errorf("list alert checks: %w", e)
	}
	defer rows.Close()
	out := []AlertCheck{}
	for rows.Next() {
		v, se := scanAlertCheck(rows)
		if se != nil {
			return nil, fmt.Errorf("scan alert check: %w", se)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) GetAlertCheck(ctx context.Context, id string) (AlertCheck, error) {
	v, e := scanAlertCheck(s.pool.QueryRow(ctx, `SELECT `+checkColumns+` FROM alert_check WHERE id=$1`, id))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return v, fmt.Errorf("get alert check: %w", e)
	}
	return v, e
}
func (s *Store) CreateAlertCheck(ctx context.Context, v AlertCheck) (AlertCheck, error) {
	if len(v.Config) == 0 {
		v.Config = json.RawMessage(`{}`)
	}
	o, e := scanAlertCheck(s.pool.QueryRow(ctx, `INSERT INTO alert_check(name,kind,target,config,interval_seconds,enabled,server_id,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+checkColumns, v.Name, v.Kind, v.Target, v.Config, v.IntervalSeconds, v.Enabled, v.ServerID, v.UpdatedBy))
	if e != nil {
		return o, fmt.Errorf("create alert check: %w", e)
	}
	return o, nil
}
func (s *Store) UpdateAlertCheck(ctx context.Context, v AlertCheck) error {
	tag, e := s.pool.Exec(ctx, `UPDATE alert_check SET name=$2,kind=$3,target=$4,config=$5,interval_seconds=$6,enabled=$7,server_id=$8,updated_at=now(),updated_by=$9 WHERE id=$1`, v.ID, v.Name, v.Kind, v.Target, v.Config, v.IntervalSeconds, v.Enabled, v.ServerID, v.UpdatedBy)
	if e != nil {
		return fmt.Errorf("update alert check: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DeleteAlertCheck(ctx context.Context, id string) error {
	tag, e := s.pool.Exec(ctx, `DELETE FROM alert_check WHERE id=$1`, id)
	if e != nil {
		return fmt.Errorf("delete alert check: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) ListEnabledAlertChecks(ctx context.Context) ([]AlertCheck, error) {
	rows, e := s.pool.Query(ctx, `SELECT `+checkColumns+` FROM alert_check WHERE enabled ORDER BY name,id`)
	if e != nil {
		return nil, fmt.Errorf("list enabled checks: %w", e)
	}
	defer rows.Close()
	out := []AlertCheck{}
	for rows.Next() {
		v, se := scanAlertCheck(rows)
		if se != nil {
			return nil, fmt.Errorf("scan enabled check: %w", se)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) InsertAlertCheckResult(ctx context.Context, v AlertCheckResult) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO alert_check_result(time,check_id,ok,latency_ms,status_code,error,cert_not_after) VALUES($1,$2,$3,$4,$5,$6,$7)`, v.Time, v.CheckID, v.OK, v.LatencyMS, v.StatusCode, v.Error, v.CertNotAfter)
	if e != nil {
		return fmt.Errorf("insert check result: %w", e)
	}
	return nil
}
func (s *Store) LatestAlertCheckResult(ctx context.Context, id string) (AlertCheckResult, error) {
	var v AlertCheckResult
	e := s.pool.QueryRow(ctx, `SELECT time,check_id,ok,latency_ms,status_code,error,cert_not_after FROM alert_check_result WHERE check_id=$1 ORDER BY time DESC LIMIT 1`, id).Scan(&v.Time, &v.CheckID, &v.OK, &v.LatencyMS, &v.StatusCode, &v.Error, &v.CertNotAfter)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if e != nil {
		return v, fmt.Errorf("get latest check result: %w", e)
	}
	return v, nil
}
func (s *Store) DeleteCheckResultsBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, e := s.pool.Exec(ctx, `DELETE FROM alert_check_result WHERE time<$1`, before)
	if e != nil {
		return 0, fmt.Errorf("delete check results: %w", e)
	}
	return tag.RowsAffected(), nil
}

func (s *Store) ListNotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	rows, e := s.pool.Query(ctx, `SELECT `+channelColumns+` FROM notification_channel ORDER BY name,id`)
	if e != nil {
		return nil, fmt.Errorf("list notification channels: %w", e)
	}
	defer rows.Close()
	out := []NotificationChannel{}
	for rows.Next() {
		v, se := scanChannel(rows)
		if se != nil {
			return nil, fmt.Errorf("scan notification channel: %w", se)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) GetNotificationChannel(ctx context.Context, id string) (NotificationChannel, error) {
	v, e := scanChannel(s.pool.QueryRow(ctx, `SELECT `+channelColumns+` FROM notification_channel WHERE id=$1`, id))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return v, fmt.Errorf("get notification channel: %w", e)
	}
	return v, e
}
func (s *Store) CreateNotificationChannel(ctx context.Context, v NotificationChannel, secret []byte) (NotificationChannel, error) {
	if len(v.Config) == 0 {
		v.Config = json.RawMessage(`{}`)
	}
	o, e := scanChannel(s.pool.QueryRow(ctx, `INSERT INTO notification_channel(name,type,enabled,language,config,secret_enc,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+channelColumns, v.Name, v.Type, v.Enabled, v.Language, v.Config, secret, v.UpdatedBy))
	if e != nil {
		return o, fmt.Errorf("create notification channel: %w", e)
	}
	return o, nil
}
func (s *Store) UpdateNotificationChannel(ctx context.Context, v NotificationChannel, secret []byte) error {
	tag, e := s.pool.Exec(ctx, `UPDATE notification_channel SET name=$2,type=$3,enabled=$4,language=$5,config=$6,secret_enc=CASE WHEN $7::bytea IS NULL THEN secret_enc ELSE $7 END,updated_at=now(),updated_by=$8 WHERE id=$1`, v.ID, v.Name, v.Type, v.Enabled, v.Language, v.Config, secret, v.UpdatedBy)
	if e != nil {
		return fmt.Errorf("update notification channel: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DeleteNotificationChannel(ctx context.Context, id string) error {
	tag, e := s.pool.Exec(ctx, `DELETE FROM notification_channel WHERE id=$1`, id)
	if e != nil {
		return fmt.Errorf("delete notification channel: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) GetChannelSecret(ctx context.Context, id string) ([]byte, error) {
	var secret []byte
	e := s.pool.QueryRow(ctx, `SELECT secret_enc FROM notification_channel WHERE id=$1`, id).Scan(&secret)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, fmt.Errorf("get channel secret: %w", e)
	}
	return secret, nil
}

func (s *Store) EnqueueDelivery(ctx context.Context, v NotificationDelivery) (NotificationDelivery, error) {
	o, e := scanDelivery(s.pool.QueryRow(ctx, `INSERT INTO notification_delivery(alert_id,channel_id,event,attempts,next_attempt_at,status,last_error) VALUES($1,$2,$3,$4,COALESCE($5,now()),'pending',$6) RETURNING `+deliveryColumns, v.AlertID, v.ChannelID, v.Event, v.Attempts, v.NextAttemptAt, v.LastError))
	if e != nil {
		return o, fmt.Errorf("enqueue delivery: %w", e)
	}
	return o, nil
}

// ClaimDue leases due deliveries for 5 minutes by pushing next_attempt_at, so a
// crashed sender gets them retried and a concurrent one never sees them.
func (s *Store) ClaimDue(ctx context.Context, now time.Time, limit int) ([]NotificationDelivery, error) {
	if limit < 1 {
		return []NotificationDelivery{}, nil
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, fmt.Errorf("begin claim deliveries: %w", e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, e := tx.Query(ctx, `WITH due AS (SELECT id FROM notification_delivery WHERE status='pending' AND next_attempt_at<=$1 ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT $2) UPDATE notification_delivery d SET attempts=d.attempts+1, next_attempt_at=$1::timestamptz + interval '5 minutes' FROM due WHERE d.id=due.id RETURNING d.id,d.alert_id,d.channel_id,d.event,d.attempts,d.next_attempt_at,d.status,d.last_error,d.created_at,d.sent_at`, now, limit)
	if e != nil {
		return nil, fmt.Errorf("claim deliveries: %w", e)
	}
	out := []NotificationDelivery{}
	for rows.Next() {
		v, se := scanDelivery(rows)
		if se != nil {
			rows.Close()
			return nil, fmt.Errorf("scan claimed delivery: %w", se)
		}
		out = append(out, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, fmt.Errorf("read claimed deliveries: %w", e)
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, fmt.Errorf("commit claimed deliveries: %w", e)
	}
	return out, nil
}
func (s *Store) MarkDeliverySent(ctx context.Context, id string, at time.Time) error {
	tag, e := s.pool.Exec(ctx, `UPDATE notification_delivery SET status='sent',sent_at=$2,last_error='' WHERE id=$1`, id, at)
	if e != nil {
		return fmt.Errorf("mark delivery sent: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) MarkDeliveryRetry(ctx context.Context, id string, next time.Time, message string) error {
	tag, e := s.pool.Exec(ctx, `UPDATE notification_delivery SET status='pending',next_attempt_at=$2,last_error=$3 WHERE id=$1`, id, next, message)
	if e != nil {
		return fmt.Errorf("retry delivery: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) MarkDeliveryFailed(ctx context.Context, id, message string) error {
	tag, e := s.pool.Exec(ctx, `UPDATE notification_delivery SET status='failed',last_error=$2 WHERE id=$1`, id, message)
	if e != nil {
		return fmt.Errorf("fail delivery: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetAllowPrivateTargets(ctx context.Context) (bool, error) {
	var allowed bool
	e := s.pool.QueryRow(ctx, `SELECT allow_private_targets FROM organization ORDER BY id LIMIT 1`).Scan(&allowed)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if e != nil {
		return false, fmt.Errorf("get private target setting: %w", e)
	}
	return allowed, nil
}
func (s *Store) SetAllowPrivateTargets(ctx context.Context, allowed bool) error {
	tag, e := s.pool.Exec(ctx, `UPDATE organization SET allow_private_targets=$1`, allowed)
	if e != nil {
		return fmt.Errorf("set private target setting: %w", e)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
