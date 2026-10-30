package store

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// App sources, build modes and deployment states.
const (
	AppSourceGit      = "git"
	AppSourceImage    = "image"
	AppSourceTemplate = "template"

	AppBuildDockerfile = "dockerfile"
	AppBuildAuto       = "auto"

	DeploymentQueued     = "queued"
	DeploymentBuilding   = "building"
	DeploymentReleasing  = "releasing"
	DeploymentSucceeded  = "succeeded"
	DeploymentFailed     = "failed"
	DeploymentSuperseded = "superseded"
)

// MaxDeploymentLog bounds the text stored for one deployment.
const MaxDeploymentLog = 256 << 10

// ErrAppExists is returned when the server already has an app with that stack
// name.
var ErrAppExists = errors.New("app already exists")

// ErrDomainExists is returned when another app already uses the hostname.
var ErrDomainExists = errors.New("domain already exists")

// App is one deployable app backed by a Compose stack on a server.
type App struct {
	ID               string
	ServerID         string
	StackName        string
	Source           string
	RepoURL          string
	Branch           string
	Build            string
	DockerfilePath   string
	BuildContext     string
	Image            string
	Service          string
	Port             int
	AutoDeploy       bool
	PollEnabled      bool
	ZeroDowntime     bool
	TemplateID       string
	TemplateVersion  string
	DeployKeyPub     string
	DeployKeyEnc     []byte
	WebhookSecretEnc []byte
	KnownHost        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	UpdatedBy        string
}

// AppEnv is one variable of an app. ValueEnc is already sealed by the caller.
type AppEnv struct {
	AppID     string
	Key       string
	ValueEnc  []byte
	Secret    bool
	BuildTime bool
}

// Deployment is one attempt to build and release an app.
type Deployment struct {
	ID            string
	AppID         string
	Number        int
	Trigger       string
	CommitSHA     string
	CommitSubject string
	ImageRef      string
	State         string
	StartedAt     time.Time
	FinishedAt    time.Time
	Error         string
	Log           string
}

// Domain is one hostname served by an app.
type Domain struct {
	ID        string
	AppID     string
	Hostname  string
	Service   string
	Port      int
	CreatedAt time.Time
}

const appColumns = `id, server_id, stack_name, source, repo_url, branch, build, dockerfile_path,
	build_context, image, service, port, auto_deploy, poll_enabled, zero_downtime, template_id,
	template_version, deploy_key_pub, deploy_key_enc, webhook_secret_enc, known_host,
	created_at, updated_at, updated_by`

const appEnvColumns = `app_id, key, value_enc, secret, build_time`

const deploymentColumns = `id, app_id, number, "trigger", commit_sha, commit_subject, image_ref,
	state, started_at, finished_at, error, log`

const domainColumns = `id, app_id, hostname, service, port, created_at`

func scanApp(row rowScanner) (App, error) {
	var app App
	err := row.Scan(&app.ID, &app.ServerID, &app.StackName, &app.Source, &app.RepoURL, &app.Branch,
		&app.Build, &app.DockerfilePath, &app.BuildContext, &app.Image, &app.Service, &app.Port,
		&app.AutoDeploy, &app.PollEnabled, &app.ZeroDowntime, &app.TemplateID, &app.TemplateVersion,
		&app.DeployKeyPub,
		&app.DeployKeyEnc, &app.WebhookSecretEnc, &app.KnownHost, &app.CreatedAt, &app.UpdatedAt,
		&app.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return App{}, ErrNotFound
	}
	return app, err
}

func scanAppEnv(row rowScanner) (AppEnv, error) {
	var env AppEnv
	err := row.Scan(&env.AppID, &env.Key, &env.ValueEnc, &env.Secret, &env.BuildTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return AppEnv{}, ErrNotFound
	}
	return env, err
}

func scanDeployment(row rowScanner) (Deployment, error) {
	var (
		deployment Deployment
		startedAt  *time.Time
		finishedAt *time.Time
	)
	err := row.Scan(&deployment.ID, &deployment.AppID, &deployment.Number, &deployment.Trigger,
		&deployment.CommitSHA, &deployment.CommitSubject, &deployment.ImageRef, &deployment.State,
		&startedAt, &finishedAt, &deployment.Error, &deployment.Log)
	if errors.Is(err, pgx.ErrNoRows) {
		return Deployment{}, ErrNotFound
	}
	if err != nil {
		return Deployment{}, err
	}
	deployment.StartedAt = timeValue(startedAt)
	deployment.FinishedAt = timeValue(finishedAt)
	return deployment, nil
}

func scanDomain(row rowScanner) (Domain, error) {
	var domain Domain
	err := row.Scan(&domain.ID, &domain.AppID, &domain.Hostname, &domain.Service, &domain.Port,
		&domain.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	return domain, err
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

// CreateApp inserts an app and returns its generated id and timestamps.
func (s *Store) CreateApp(ctx context.Context, app App) (App, error) {
	created, err := scanApp(s.pool.QueryRow(ctx,
		`INSERT INTO app
		     (server_id, stack_name, source, repo_url, branch, build, dockerfile_path,
		      build_context, image, service, port, auto_deploy, poll_enabled, zero_downtime,
		      template_id, template_version, deploy_key_pub, deploy_key_enc, webhook_secret_enc,
		      known_host, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
		         $17, $18, $19, $20, $21)
		 RETURNING `+appColumns,
		app.ServerID, app.StackName, app.Source, app.RepoURL, app.Branch, app.Build,
		app.DockerfilePath, app.BuildContext, app.Image, app.Service, app.Port, app.AutoDeploy,
		app.PollEnabled, app.ZeroDowntime, app.TemplateID, app.TemplateVersion, app.DeployKeyPub,
		app.DeployKeyEnc, app.WebhookSecretEnc, app.KnownHost, app.UpdatedBy))
	if isUniqueViolation(err) {
		return App{}, ErrAppExists
	}
	if err != nil {
		return App{}, fmt.Errorf("create app %q: %w", app.StackName, err)
	}
	return created, nil
}

// GetApp returns one app or ErrNotFound.
func (s *Store) GetApp(ctx context.Context, id string) (App, error) {
	app, err := scanApp(s.pool.QueryRow(ctx, `SELECT `+appColumns+` FROM app WHERE id = $1`, id))
	if errors.Is(err, ErrNotFound) {
		return App{}, ErrNotFound
	}
	if err != nil {
		return App{}, fmt.Errorf("get app %q: %w", id, err)
	}
	return app, nil
}

// ListApps returns every app ordered by server and stack name.
func (s *Store) ListApps(ctx context.Context) ([]App, error) {
	return s.listApps(ctx, `SELECT `+appColumns+` FROM app ORDER BY server_id, stack_name, id`)
}

// ListAppsByServer returns the apps of one server ordered by stack name.
func (s *Store) ListAppsByServer(ctx context.Context, serverID string) ([]App, error) {
	return s.listApps(ctx,
		`SELECT `+appColumns+` FROM app WHERE server_id = $1 ORDER BY stack_name, id`, serverID)
}

// ListPollableApps returns the Git apps that asked for automatic polling.
func (s *Store) ListPollableApps(ctx context.Context) ([]App, error) {
	return s.listApps(ctx,
		`SELECT `+appColumns+` FROM app
		 WHERE source = $1 AND auto_deploy AND poll_enabled
		 ORDER BY server_id, stack_name, id`, AppSourceGit)
}

func (s *Store) listApps(ctx context.Context, query string, args ...any) ([]App, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	defer rows.Close()

	apps := make([]App, 0, 8)
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			return nil, fmt.Errorf("scan app: %w", err)
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read apps: %w", err)
	}
	return apps, nil
}

// UpdateApp replaces the editable fields of an app and returns the stored row.
func (s *Store) UpdateApp(ctx context.Context, app App) (App, error) {
	updated, err := scanApp(s.pool.QueryRow(ctx,
		`UPDATE app SET
		     server_id = $2, stack_name = $3, source = $4, repo_url = $5, branch = $6,
		     build = $7, dockerfile_path = $8, build_context = $9, image = $10, service = $11,
		     port = $12, auto_deploy = $13, poll_enabled = $14, zero_downtime = $15,
		     template_id = $16, template_version = $17, deploy_key_pub = $18,
		     deploy_key_enc = $19, webhook_secret_enc = $20, known_host = $21,
		     updated_at = now(), updated_by = $22
		 WHERE id = $1
		 RETURNING `+appColumns,
		app.ID, app.ServerID, app.StackName, app.Source, app.RepoURL, app.Branch, app.Build,
		app.DockerfilePath, app.BuildContext, app.Image, app.Service, app.Port, app.AutoDeploy,
		app.PollEnabled, app.ZeroDowntime, app.TemplateID, app.TemplateVersion, app.DeployKeyPub,
		app.DeployKeyEnc, app.WebhookSecretEnc, app.KnownHost, app.UpdatedBy))
	if errors.Is(err, ErrNotFound) {
		return App{}, ErrNotFound
	}
	if isUniqueViolation(err) {
		return App{}, ErrAppExists
	}
	if err != nil {
		return App{}, fmt.Errorf("update app %q: %w", app.ID, err)
	}
	return updated, nil
}

// DeleteApp removes an app. Its variables, deployments and domains go with it.
func (s *Store) DeleteApp(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM app WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete app %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAppEnv inserts or replaces one variable.
func (s *Store) SetAppEnv(ctx context.Context, env AppEnv) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO app_env (app_id, key, value_enc, secret, build_time)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (app_id, key) DO UPDATE SET
		     value_enc = EXCLUDED.value_enc,
		     secret = EXCLUDED.secret,
		     build_time = EXCLUDED.build_time`,
		env.AppID, env.Key, env.ValueEnc, env.Secret, env.BuildTime)
	if err != nil {
		return fmt.Errorf("set app env %q: %w", env.Key, err)
	}
	return nil
}

// DeleteAppEnv removes one variable.
func (s *Store) DeleteAppEnv(ctx context.Context, appID, key string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM app_env WHERE app_id = $1 AND key = $2`, appID, key)
	if err != nil {
		return fmt.Errorf("delete app env %q: %w", key, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAppEnv returns every variable with its sealed value, for a deploy.
func (s *Store) ListAppEnv(ctx context.Context, appID string) ([]AppEnv, error) {
	return s.listAppEnv(ctx,
		`SELECT `+appEnvColumns+` FROM app_env WHERE app_id = $1 ORDER BY key`, appID)
}

// ListAppEnvPublic returns every variable but leaves ValueEnc nil for secret
// ones, so they can be shown without their sealed values.
func (s *Store) ListAppEnvPublic(ctx context.Context, appID string) ([]AppEnv, error) {
	return s.listAppEnv(ctx,
		`SELECT app_id, key, CASE WHEN secret THEN NULL ELSE value_enc END, secret, build_time
		 FROM app_env WHERE app_id = $1 ORDER BY key`, appID)
}

func (s *Store) listAppEnv(ctx context.Context, query string, args ...any) ([]AppEnv, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list app env: %w", err)
	}
	defer rows.Close()

	envs := make([]AppEnv, 0, 8)
	for rows.Next() {
		env, err := scanAppEnv(rows)
		if err != nil {
			return nil, fmt.Errorf("scan app env: %w", err)
		}
		envs = append(envs, env)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read app env: %w", err)
	}
	return envs, nil
}

// CreateDeployment assigns the next number of the app and stores the
// deployment. The app row is locked so two concurrent calls cannot take the
// same number.
func (s *Store) CreateDeployment(ctx context.Context, deployment Deployment) (Deployment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Deployment{}, fmt.Errorf("begin deployment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var appID string
	if err := tx.QueryRow(ctx, `SELECT id FROM app WHERE id = $1 FOR UPDATE`, deployment.AppID).Scan(&appID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Deployment{}, ErrNotFound
		}
		return Deployment{}, fmt.Errorf("lock app for deployment: %w", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM deployment WHERE app_id = $1`,
		deployment.AppID).Scan(&deployment.Number); err != nil {
		return Deployment{}, fmt.Errorf("assign deployment number: %w", err)
	}
	if deployment.State == "" {
		deployment.State = DeploymentQueued
	}

	created, err := scanDeployment(tx.QueryRow(ctx,
		`INSERT INTO deployment
		     (app_id, number, "trigger", commit_sha, commit_subject, image_ref, state,
		      started_at, finished_at, error, log)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING `+deploymentColumns,
		deployment.AppID, deployment.Number, deployment.Trigger, deployment.CommitSHA,
		deployment.CommitSubject, deployment.ImageRef, deployment.State,
		nullableTime(deployment.StartedAt), nullableTime(deployment.FinishedAt), deployment.Error,
		capDeploymentLog(deployment.Log)))
	if err != nil {
		return Deployment{}, fmt.Errorf("create deployment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Deployment{}, fmt.Errorf("commit deployment: %w", err)
	}
	return created, nil
}

// GetDeployment returns one deployment or ErrNotFound.
func (s *Store) GetDeployment(ctx context.Context, id string) (Deployment, error) {
	deployment, err := scanDeployment(s.pool.QueryRow(ctx,
		`SELECT `+deploymentColumns+` FROM deployment WHERE id = $1`, id))
	if errors.Is(err, ErrNotFound) {
		return Deployment{}, ErrNotFound
	}
	if err != nil {
		return Deployment{}, fmt.Errorf("get deployment %q: %w", id, err)
	}
	return deployment, nil
}

// UpdateDeployment replaces the mutable state of a deployment. The caller
// passes the whole row so a state change never silently keeps a stale log.
func (s *Store) UpdateDeployment(ctx context.Context, deployment Deployment) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE deployment SET
		     state = $2, error = $3, started_at = $4, finished_at = $5, log = $6
		 WHERE id = $1`,
		deployment.ID, deployment.State, deployment.Error, nullableTime(deployment.StartedAt),
		nullableTime(deployment.FinishedAt), capDeploymentLog(deployment.Log))
	if err != nil {
		return fmt.Errorf("update deployment %q: %w", deployment.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AppendDeploymentLog appends text and keeps the end when the 256 KiB limit is
// crossed.
func (s *Store) AppendDeploymentLog(ctx context.Context, id, chunk string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("append deployment log: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current string
	if err := tx.QueryRow(ctx, `SELECT log FROM deployment WHERE id = $1 FOR UPDATE`, id).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read deployment log: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE deployment SET log = $2 WHERE id = $1`,
		id, capDeploymentLog(current+chunk)); err != nil {
		return fmt.Errorf("write deployment log: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit deployment log: %w", err)
	}
	return nil
}

// capDeploymentLog keeps the last MaxDeploymentLog bytes without splitting a
// UTF-8 character.
func capDeploymentLog(log string) string {
	if len(log) <= MaxDeploymentLog {
		return log
	}
	start := len(log) - MaxDeploymentLog
	for start < len(log) && !utf8.RuneStart(log[start]) {
		start++
	}
	return log[start:]
}

// ListDeployments returns the deployments of an app, newest first.
func (s *Store) ListDeployments(ctx context.Context, appID string, limit int) ([]Deployment, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+deploymentColumns+` FROM deployment
		 WHERE app_id = $1 ORDER BY number DESC LIMIT $2`, appID, limit)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	defer rows.Close()

	deployments := make([]Deployment, 0, limit)
	for rows.Next() {
		deployment, err := scanDeployment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan deployment: %w", err)
		}
		deployments = append(deployments, deployment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read deployments: %w", err)
	}
	return deployments, nil
}

// SupersedeQueuedDeployments marks the waiting deployments of an app as
// superseded. A deployment already building or releasing is never touched.
func (s *Store) SupersedeQueuedDeployments(ctx context.Context, appID string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE deployment SET state = $2 WHERE app_id = $1 AND state = $3`,
		appID, DeploymentSuperseded, DeploymentQueued)
	if err != nil {
		return 0, fmt.Errorf("supersede queued deployments: %w", err)
	}
	return tag.RowsAffected(), nil
}

// AddDomain adds a hostname to an app. Hostnames are unique across all apps.
func (s *Store) AddDomain(ctx context.Context, domain Domain) (Domain, error) {
	created, err := scanDomain(s.pool.QueryRow(ctx,
		`INSERT INTO domain (app_id, hostname, service, port)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+domainColumns,
		domain.AppID, domain.Hostname, domain.Service, domain.Port))
	if isUniqueViolation(err) {
		return Domain{}, ErrDomainExists
	}
	if err != nil {
		return Domain{}, fmt.Errorf("add domain %q: %w", domain.Hostname, err)
	}
	return created, nil
}

// RemoveDomain deletes a domain by id.
func (s *Store) RemoveDomain(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM domain WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("remove domain %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListDomains returns every domain ordered by hostname.
func (s *Store) ListDomains(ctx context.Context) ([]Domain, error) {
	return s.listDomains(ctx, `SELECT `+domainColumns+` FROM domain ORDER BY hostname, id`)
}

// ListAppDomains returns the domains of one app ordered by hostname.
func (s *Store) ListAppDomains(ctx context.Context, appID string) ([]Domain, error) {
	return s.listDomains(ctx,
		`SELECT `+domainColumns+` FROM domain WHERE app_id = $1 ORDER BY hostname, id`, appID)
}

func (s *Store) listDomains(ctx context.Context, query string, args ...any) ([]Domain, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer rows.Close()

	domains := make([]Domain, 0, 8)
	for rows.Next() {
		domain, err := scanDomain(rows)
		if err != nil {
			return nil, fmt.Errorf("scan domain: %w", err)
		}
		domains = append(domains, domain)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read domains: %w", err)
	}
	return domains, nil
}
