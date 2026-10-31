package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/ssh"

	"github.com/catlinux/Talaia/core/internal/deploy"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/templates"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// AppStore is the part of the store the app API needs.
type AppStore interface {
	CreateApp(ctx context.Context, app store.App) (store.App, error)
	UpdateApp(ctx context.Context, app store.App) (store.App, error)
	DeleteApp(ctx context.Context, id string) error
	GetApp(ctx context.Context, id string) (store.App, error)
	ListApps(ctx context.Context) ([]store.App, error)
	ListAppsByServer(ctx context.Context, serverID string) ([]store.App, error)
	SetAppEnv(ctx context.Context, env store.AppEnv) error
	DeleteAppEnv(ctx context.Context, appID, key string) error
	ListAppEnvPublic(ctx context.Context, appID string) ([]store.AppEnv, error)
	AddDomain(ctx context.Context, domain store.Domain) (store.Domain, error)
	RemoveDomain(ctx context.Context, id string) error
	GetDomain(ctx context.Context, id string) (store.Domain, error)
	ListAppDomains(ctx context.Context, appID string) ([]store.Domain, error)
	ListDomains(ctx context.Context) ([]store.Domain, error)
	ListDeployments(ctx context.Context, appID string, limit int) ([]store.Deployment, error)
	GetDeployment(ctx context.Context, id string) (store.Deployment, error)
}

// AppSecretBox seals the app secrets at rest.
type AppSecretBox interface {
	Seal(plaintext []byte) []byte
	Open(sealed []byte) ([]byte, error)
}

// DeploymentQueue accepts new deployment work.
type DeploymentQueue interface {
	Enqueue(ctx context.Context, appID, trigger, commit string) error
}

// AppCleaner removes the server resources of an app before its rows are
// deleted. HubAgent implements it.
type AppCleaner interface {
	RemoveStack(ctx context.Context, serverID, name string) (string, error)
	CleanupApp(ctx context.Context, serverID, name string) (string, error)
	ApplyProxyRoutes(ctx context.Context, serverID string, routes []*agentv1.ProxyRoute) (string, error)
}

// HostResolver resolves domain names for the warning of a new domain.
type HostResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// AppDeps are the collaborators of the app, variable, domain and deployment
// endpoints.
type AppDeps struct {
	Store     AppStore
	Box       AppSecretBox
	Queue     DeploymentQueue
	Cleaner   AppCleaner
	Audit     Auditor
	Servers   ServerLister
	Agents    LiveAgents
	Resolver  HostResolver
	PublicURL string
	Templates *templates.Catalog
}

type appHandler struct {
	deps AppDeps
}

var (
	appStackRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	appServiceRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	appEnvKeyRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

const (
	appDeleteStackTimeout   = 10 * time.Minute
	appDeleteCleanupTimeout = 5 * time.Minute
	appDeleteProxyTimeout   = time.Minute
)

type appDeleteRequest struct {
	ConfirmName string `json:"confirm_name"`
}

type appDeleteResponse struct {
	Output string `json:"output"`
}

type AppResponse struct {
	ID              string    `json:"id"`
	ServerID        string    `json:"server_id"`
	StackName       string    `json:"stack_name"`
	Source          string    `json:"source"`
	RepoURL         string    `json:"repo_url"`
	Branch          string    `json:"branch"`
	Build           string    `json:"build"`
	DockerfilePath  string    `json:"dockerfile_path"`
	BuildContext    string    `json:"build_context"`
	Image           string    `json:"image"`
	Service         string    `json:"service"`
	Port            int       `json:"port"`
	HostPort        int       `json:"host_port"`
	AutoDeploy      bool      `json:"auto_deploy"`
	PollEnabled     bool      `json:"poll_enabled"`
	ZeroDowntime    bool      `json:"zero_downtime"`
	TemplateID      string    `json:"template_id"`
	TemplateVersion string    `json:"template_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	UpdatedBy       string    `json:"updated_by"`
}

type appCreatedResponse struct {
	AppResponse
	DeployKeyPublic string `json:"deploy_key_public,omitempty"`
	WebhookSecret   string `json:"webhook_secret,omitempty"`
	WebhookURL      string `json:"webhook_url,omitempty"`
}

func toAppResponse(app store.App) AppResponse {
	return AppResponse{
		ID: app.ID, ServerID: app.ServerID, StackName: app.StackName, Source: app.Source,
		RepoURL: app.RepoURL, Branch: app.Branch, Build: app.Build,
		DockerfilePath: app.DockerfilePath, BuildContext: app.BuildContext, Image: app.Image,
		Service: app.Service, Port: app.Port, HostPort: app.HostPort, AutoDeploy: app.AutoDeploy,
		PollEnabled: app.PollEnabled, ZeroDowntime: app.ZeroDowntime, TemplateID: app.TemplateID,
		TemplateVersion: app.TemplateVersion, CreatedAt: app.CreatedAt, UpdatedAt: app.UpdatedAt,
		UpdatedBy: app.UpdatedBy,
	}
}

type appCreateRequest struct {
	StackName       string `json:"stack_name"`
	Source          string `json:"source"`
	RepoURL         string `json:"repo_url"`
	Branch          string `json:"branch"`
	Build           string `json:"build"`
	DockerfilePath  string `json:"dockerfile_path"`
	BuildContext    string `json:"build_context"`
	Image           string `json:"image"`
	Service         string `json:"service"`
	Port            int    `json:"port"`
	AutoDeploy      *bool  `json:"auto_deploy"`
	PollEnabled     *bool  `json:"poll_enabled"`
	ZeroDowntime    *bool  `json:"zero_downtime"`
	TemplateID      string `json:"template_id"`
	TemplateVersion string `json:"template_version"`
}

type appUpdateRequest struct {
	Source          *string `json:"source"`
	RepoURL         *string `json:"repo_url"`
	Branch          *string `json:"branch"`
	Build           *string `json:"build"`
	DockerfilePath  *string `json:"dockerfile_path"`
	BuildContext    *string `json:"build_context"`
	Image           *string `json:"image"`
	Service         *string `json:"service"`
	Port            *int    `json:"port"`
	AutoDeploy      *bool   `json:"auto_deploy"`
	PollEnabled     *bool   `json:"poll_enabled"`
	ZeroDowntime    *bool   `json:"zero_downtime"`
	TemplateID      *string `json:"template_id"`
	TemplateVersion *string `json:"template_version"`
}

func NewAppsListHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.list)
}

func NewAppsByServerHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.listByServer)
}

func NewAppCreateHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.create)
}

func NewAppHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.get)
}

func NewAppUpdateHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.update)
}

func NewAppDeleteHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.delete)
}

func (h appHandler) list(w http.ResponseWriter, r *http.Request) {
	apps, err := h.deps.Store.ListApps(r.Context())
	if err != nil {
		internal(w, "list apps", err)
		return
	}
	h.writeList(w, r, apps)
}

func (h appHandler) listByServer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	apps, err := h.deps.Store.ListAppsByServer(r.Context(), serverID)
	if err != nil {
		internal(w, "list apps by server", err)
		return
	}
	h.writeList(w, r, apps)
}

func (h appHandler) writeList(w http.ResponseWriter, r *http.Request, apps []store.App) {
	effective, ok := effectiveFromRequest(r)
	if !ok {
		writeJSON(w, http.StatusOK, []AppResponse{})
		return
	}
	groups := map[string]string{}
	if h.deps.Servers != nil {
		if servers, err := h.deps.Servers.ListServers(r.Context()); err == nil {
			for _, server := range servers {
				groups[server.ID] = server.GroupID
			}
		} else {
			internal(w, "list servers for apps", err)
			return
		}
	}
	response := make([]AppResponse, 0, len(apps))
	for _, app := range apps {
		target := rbac.Target{ServerID: app.ServerID, GroupID: groups[app.ServerID], Stack: app.StackName}
		if effective.Can(rbac.PermStackView, target) {
			response = append(response, toAppResponse(app))
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (h appHandler) get(w http.ResponseWriter, r *http.Request) {
	app, err := h.deps.Store.GetApp(r.Context(), chi.URLParam(r, "appID"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		internal(w, "get app", err)
		return
	}
	writeJSON(w, http.StatusOK, toAppResponse(app))
}

func (h appHandler) create(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	var request appCreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	app := store.App{
		ServerID: serverID, StackName: request.StackName, Source: request.Source,
		RepoURL: request.RepoURL, Branch: request.Branch, Build: request.Build,
		DockerfilePath: request.DockerfilePath, BuildContext: request.BuildContext,
		Image: request.Image, Service: request.Service, Port: request.Port,
		TemplateID: request.TemplateID, TemplateVersion: request.TemplateVersion,
		AutoDeploy: true, ZeroDowntime: true,
	}
	if request.AutoDeploy != nil {
		app.AutoDeploy = *request.AutoDeploy
	}
	if request.PollEnabled != nil {
		app.PollEnabled = *request.PollEnabled
	}
	if request.ZeroDowntime != nil {
		app.ZeroDowntime = *request.ZeroDowntime
	}
	if app.Build == "" {
		app.Build = store.AppBuildDockerfile
	}
	if app.Service == "" {
		app.Service = "app"
	}
	if app.DockerfilePath == "" {
		app.DockerfilePath = "Dockerfile"
	}
	if app.BuildContext == "" {
		app.BuildContext = "."
	}
	if app.Source == "" {
		app.Source = store.AppSourceGit
	}
	if app.Source == store.AppSourceGit && strings.TrimSpace(app.Branch) == "" {
		app.Branch = "main"
	}
	if app.Source == store.AppSourceTemplate {
		if h.deps.Templates == nil {
			writeError(w, http.StatusBadRequest, "template_unavailable")
			return
		}
		template, ok := h.deps.Templates.Get(request.TemplateID)
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown_template")
			return
		}
		if request.TemplateVersion != "" && request.TemplateVersion != template.Version {
			writeError(w, http.StatusBadRequest, "unknown_template_version")
			return
		}
		app.TemplateID = template.ID
		app.TemplateVersion = template.Version
		app.Image = template.Image
		app.Service = template.Service
		app.Port = template.Port
		app.Build = store.AppBuildDockerfile
	}
	if err := validateApp(app); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if session, ok := sessionFromContext(r.Context()); ok {
		app.UpdatedBy = session.User.Username
	}
	publicKey, webhookSecret := "", ""
	if app.Source == store.AppSourceGit {
		var err error
		publicKey, webhookSecret = "", ""
		publicKey, app.DeployKeyEnc, webhookSecret, app.WebhookSecretEnc, err = newAppSecrets(h.deps.Box)
		if err != nil {
			internal(w, "generate app secrets", err)
			return
		}
		app.DeployKeyPub = publicKey
		app.KnownHost = "github.com"
	}
	created, err := h.deps.Store.CreateApp(r.Context(), app)
	if errors.Is(err, store.ErrAppExists) {
		writeError(w, http.StatusConflict, "app_exists")
		return
	}
	if err != nil {
		internal(w, "create app", err)
		return
	}
	if created.Source == store.AppSourceTemplate {
		if h.deps.Box == nil {
			_ = h.deps.Store.DeleteApp(r.Context(), created.ID)
			internal(w, "create app template password", errors.New("no secret box"))
			return
		}
		password := make([]byte, 48)
		if _, err := rand.Read(password); err != nil {
			_ = h.deps.Store.DeleteApp(r.Context(), created.ID)
			internal(w, "create app template password", err)
			return
		}
		if err := h.deps.Store.SetAppEnv(r.Context(), store.AppEnv{
			AppID: created.ID, Key: "TEMPLATE_PASSWORD",
			ValueEnc: h.deps.Box.Seal([]byte(hex.EncodeToString(password))), Secret: true,
		}); err != nil {
			_ = h.deps.Store.DeleteApp(r.Context(), created.ID)
			internal(w, "store app template password", err)
			return
		}
	}
	h.audit(r, "app_created", map[string]any{
		"app_id": created.ID, "server_id": created.ServerID, "stack": created.StackName,
	})
	if h.deps.Queue != nil && created.Source != store.AppSourceTemplate {
		if err := h.deps.Queue.Enqueue(r.Context(), created.ID, "create", ""); err != nil {
			slog.Warn("enqueue the first deployment", "app_id", created.ID, "error", err)
		} else {
			h.audit(r, "deployment_requested", map[string]any{
				"app_id": created.ID, "server_id": created.ServerID, "stack": created.StackName,
				"trigger": "create", "commit": "",
			})
		}
	}
	response := appCreatedResponse{
		AppResponse:     toAppResponse(created),
		DeployKeyPublic: publicKey,
		WebhookSecret:   webhookSecret,
		WebhookURL:      h.webhookURL(created.ID),
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, response)
}

func (h appHandler) update(w http.ResponseWriter, r *http.Request) {
	app, err := h.deps.Store.GetApp(r.Context(), chi.URLParam(r, "appID"))
	if err != nil {
		appStoreError(w, err)
		return
	}
	var request appUpdateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	before := app
	applyAppUpdate(&app, request)
	if session, ok := sessionFromContext(r.Context()); ok {
		app.UpdatedBy = session.User.Username
	}
	if err := validateApp(app); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if app.Source == store.AppSourceGit && len(app.DeployKeyEnc) == 0 {
		publicKey, privateEnc, _, webhookEnc, err := newAppSecrets(h.deps.Box)
		if err != nil {
			internal(w, "generate app secrets", err)
			return
		}
		app.DeployKeyPub, app.DeployKeyEnc = publicKey, privateEnc
		if len(app.WebhookSecretEnc) == 0 {
			app.WebhookSecretEnc = webhookEnc
		}
		app.KnownHost = "github.com"
	}
	updated, err := h.deps.Store.UpdateApp(r.Context(), app)
	if errors.Is(err, store.ErrAppExists) {
		writeError(w, http.StatusConflict, "app_exists")
		return
	}
	if err != nil {
		appStoreError(w, err)
		return
	}
	action := "app_updated"
	if appSourceChanged(before, updated) {
		action = "app_source_changed"
	}
	h.audit(r, action, map[string]any{"app_id": updated.ID, "server_id": updated.ServerID, "stack": updated.StackName})
	writeJSON(w, http.StatusOK, toAppResponse(updated))
}

func (h appHandler) delete(w http.ResponseWriter, r *http.Request) {
	app, err := h.deps.Store.GetApp(r.Context(), chi.URLParam(r, "appID"))
	if err != nil {
		appStoreError(w, err)
		return
	}

	var request appDeleteRequest
	if err := decodeJSON(w, r, &request); err != nil || request.ConfirmName != app.StackName {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	deployments, err := h.deps.Store.ListDeployments(r.Context(), app.ID, 500)
	if err != nil {
		internal(w, "list app deployments", err)
		return
	}
	for _, deployment := range deployments {
		if deployment.State == store.DeploymentBuilding || deployment.State == store.DeploymentReleasing {
			writeError(w, http.StatusConflict, "deployment_running")
			return
		}
	}

	if h.deps.Agents == nil {
		writeError(w, http.StatusConflict, "agent_offline")
		return
	}
	agent, connected := connectedAgent(h.deps.Agents, app.ServerID)
	if !connected {
		writeError(w, http.StatusConflict, "agent_offline")
		return
	}
	if h.deps.Cleaner == nil {
		writeError(w, http.StatusServiceUnavailable, "app_cleanup_unavailable")
		return
	}

	stackCtx, cancel := context.WithTimeout(r.Context(), appDeleteStackTimeout)
	stackOutput, err := h.deps.Cleaner.RemoveStack(stackCtx, app.ServerID, app.StackName)
	cancel()
	if err != nil && !errors.Is(err, deploy.ErrStackNotFound) {
		appCleanupFailed(w, "remove the app stack", err)
		return
	}

	cleanupCtx, cancel := context.WithTimeout(r.Context(), appDeleteCleanupTimeout)
	cleanupOutput, err := h.deps.Cleaner.CleanupApp(cleanupCtx, app.ServerID, app.StackName)
	cancel()
	if err != nil {
		appCleanupFailed(w, "remove the app images and build workspace", err)
		return
	}

	proxyOutput := ""
	if agent.ProxyMode == agentv1.ProxyMode_PROXY_MODE_MANAGED {
		routes, err := proxyRoutesExcept(r.Context(), h.deps.Store, app.ServerID, app.ID)
		if err != nil {
			internal(w, "build the proxy routes", err)
			return
		}
		proxyCtx, cancel := context.WithTimeout(r.Context(), appDeleteProxyTimeout)
		proxyOutput, err = h.deps.Cleaner.ApplyProxyRoutes(proxyCtx, app.ServerID, routes)
		cancel()
		if err != nil {
			appCleanupFailed(w, "apply the proxy routes", err)
			return
		}
	}

	if err := h.deps.Store.DeleteApp(r.Context(), app.ID); err != nil {
		appStoreError(w, err)
		return
	}
	h.audit(r, "app_deleted", map[string]any{
		"app_id": app.ID, "server_id": app.ServerID, "stack": app.StackName,
		"removed_images": removedImageRefs(cleanupOutput), "volumes_kept": true,
	})

	output := appDeleteOutput(stackOutput, cleanupOutput, proxyOutput)
	writeJSON(w, http.StatusOK, appDeleteResponse{Output: output})
}

// appCleanupFailed reports which agent or server step failed without exposing
// the command output, which may contain app data.
func appCleanupFailed(w http.ResponseWriter, step string, err error) {
	slog.Error("app cleanup failed", "step", step, "error", err)
	writeJSON(w, http.StatusInternalServerError, errorBody{Error: errorDetail{
		Code: "app_cleanup_failed", Reason: step,
	}})
}

func appDeleteOutput(stackOutput, cleanupOutput, proxyOutput string) string {
	parts := make([]string, 0, 3)
	for _, output := range []string{stackOutput, cleanupOutput, proxyOutput} {
		if output = strings.TrimSpace(output); output != "" {
			parts = append(parts, output)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "the app was removed")
	}
	parts = append(parts, "Named volumes were kept.")
	return strings.Join(parts, "\n")
}

// removedImageRefs extracts the image references from the cleanup output for
// the audit details. The output stays in the response.
func removedImageRefs(output string) []string {
	refs := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if ref, ok := strings.CutPrefix(line, "removed image "); ok && ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func applyAppUpdate(app *store.App, request appUpdateRequest) {
	if request.Source != nil {
		app.Source = *request.Source
	}
	if request.RepoURL != nil {
		app.RepoURL = *request.RepoURL
	}
	if request.Branch != nil {
		app.Branch = *request.Branch
	}
	if request.Build != nil {
		app.Build = *request.Build
	}
	if request.DockerfilePath != nil {
		app.DockerfilePath = *request.DockerfilePath
	}
	if request.BuildContext != nil {
		app.BuildContext = *request.BuildContext
	}
	if request.Image != nil {
		app.Image = *request.Image
	}
	if request.Service != nil {
		app.Service = *request.Service
	}
	if request.Port != nil {
		app.Port = *request.Port
	}
	if request.AutoDeploy != nil {
		app.AutoDeploy = *request.AutoDeploy
	}
	if request.PollEnabled != nil {
		app.PollEnabled = *request.PollEnabled
	}
	if request.ZeroDowntime != nil {
		app.ZeroDowntime = *request.ZeroDowntime
	}
	if request.TemplateID != nil {
		app.TemplateID = *request.TemplateID
	}
	if request.TemplateVersion != nil {
		app.TemplateVersion = *request.TemplateVersion
	}
}

func validateApp(app store.App) error {
	if !appStackRe.MatchString(app.StackName) {
		return errors.New("invalid stack name")
	}
	if app.Source != store.AppSourceGit && app.Source != store.AppSourceImage && app.Source != store.AppSourceTemplate {
		return errors.New("invalid app source")
	}
	if !appServiceRe.MatchString(app.Service) {
		return errors.New("invalid service")
	}
	if app.Port < 0 || app.Port > 65535 {
		return errors.New("invalid port")
	}
	if app.Source == store.AppSourceTemplate {
		if strings.TrimSpace(app.TemplateID) == "" || strings.TrimSpace(app.TemplateVersion) == "" ||
			strings.TrimSpace(app.Image) == "" || app.Port <= 0 {
			return errors.New("invalid template")
		}
		return nil
	}
	if app.Build != store.AppBuildDockerfile && app.Build != store.AppBuildAuto {
		return errors.New("invalid build mode")
	}
	if app.Source == store.AppSourceGit {
		if !validGitHubURL(app.RepoURL) || strings.TrimSpace(app.Branch) == "" {
			return errors.New("invalid repository")
		}
	}
	if app.Source == store.AppSourceImage && strings.TrimSpace(app.Image) == "" {
		return errors.New("invalid image")
	}
	return nil
}

func validGitHubURL(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "https://github.com/") ||
		strings.HasPrefix(value, "git@github.com:") ||
		strings.HasPrefix(value, "ssh://git@github.com/")
}

func appSourceChanged(before, after store.App) bool {
	return before.Source != after.Source || before.RepoURL != after.RepoURL ||
		before.Branch != after.Branch || before.Build != after.Build ||
		before.DockerfilePath != after.DockerfilePath || before.BuildContext != after.BuildContext ||
		before.Image != after.Image || before.TemplateID != after.TemplateID ||
		before.TemplateVersion != after.TemplateVersion
}

func appStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	internal(w, "app store", err)
}

func newAppSecrets(box AppSecretBox) (publicKey string, privateEnc []byte, webhookSecret string, webhookEnc []byte, err error) {
	if box == nil {
		return "", nil, "", nil, errors.New("no secret box")
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("generate deploy key: %w", err)
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("encode deploy public key: %w", err)
	}
	// OpenSSH format: git runs ssh -i with this file, and OpenSSH does not read
	// every ed25519 key stored as PKCS#8.
	privateBlock, err := ssh.MarshalPrivateKey(private, "talaia deploy key")
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("encode deploy private key: %w", err)
	}
	privatePEM := pem.EncodeToMemory(privateBlock)

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, "", nil, fmt.Errorf("generate webhook secret: %w", err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic))),
		box.Seal(privatePEM), hex.EncodeToString(secret), box.Seal(secret), nil
}

func (h appHandler) webhookURL(appID string) string {
	path := "/api/v1/hooks/git/" + appID
	if strings.TrimSpace(h.deps.PublicURL) == "" {
		return path
	}
	return strings.TrimRight(h.deps.PublicURL, "/") + path
}

func (h appHandler) audit(r *http.Request, action string, details map[string]any) {
	if h.deps.Audit == nil {
		return
	}
	session, _ := sessionFromContext(r.Context())
	scope := map[string]any{"type": "app"}
	if serverID, ok := details["server_id"]; ok {
		scope["server_id"] = serverID
	}
	if stack, ok := details["stack"]; ok {
		scope["stack"] = stack
	}
	_ = h.deps.Audit.AppendAudit(r.Context(), store.AuditEvent{
		Actor: session.User.Username, ActorID: session.User.ID, Action: action,
		IP: clientIP(r), Result: "success", Details: details, Scope: scope,
	})
}

func (h appHandler) app(r *http.Request) (store.App, error) {
	return h.deps.Store.GetApp(r.Context(), chi.URLParam(r, "appID"))
}

type appEnvResponse struct {
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	Secret    bool   `json:"secret"`
	BuildTime bool   `json:"build_time"`
}

type appEnvRequest struct {
	Value     string `json:"value"`
	Secret    bool   `json:"secret"`
	BuildTime bool   `json:"build_time"`
}

func NewAppEnvListHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.listEnv)
}

func NewAppEnvSetHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.setEnv)
}

func NewAppEnvDeleteHandler(deps AppDeps) http.Handler {
	h := appHandler{deps: deps}
	return http.HandlerFunc(h.deleteEnv)
}

func (h appHandler) listEnv(w http.ResponseWriter, r *http.Request) {
	app, err := h.app(r)
	if err != nil {
		appStoreError(w, err)
		return
	}
	envs, err := h.deps.Store.ListAppEnvPublic(r.Context(), app.ID)
	if err != nil {
		internal(w, "list app variables", err)
		return
	}
	response := make([]appEnvResponse, 0, len(envs))
	for _, env := range envs {
		item := appEnvResponse{Key: env.Key, Secret: env.Secret, BuildTime: env.BuildTime}
		if !env.Secret && len(env.ValueEnc) > 0 {
			if h.deps.Box == nil {
				internal(w, "open app variable", errors.New("no secret box"))
				return
			}
			value, err := h.deps.Box.Open(env.ValueEnc)
			if err != nil {
				internal(w, "open app variable", err)
				return
			}
			item.Value = string(value)
		}
		response = append(response, item)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h appHandler) setEnv(w http.ResponseWriter, r *http.Request) {
	app, err := h.app(r)
	if err != nil {
		appStoreError(w, err)
		return
	}
	key := chi.URLParam(r, "key")
	if !appEnvKeyRe.MatchString(key) {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	var request appEnvRequest
	if err := decodeJSON(w, r, &request); err != nil || len(request.Value) > 64<<10 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if h.deps.Box == nil {
		internal(w, "seal app variable", errors.New("no secret box"))
		return
	}
	if err := h.deps.Store.SetAppEnv(r.Context(), store.AppEnv{
		AppID: app.ID, Key: key, ValueEnc: h.deps.Box.Seal([]byte(request.Value)),
		Secret: request.Secret, BuildTime: request.BuildTime,
	}); err != nil {
		internal(w, "set app variable", err)
		return
	}
	h.audit(r, "app_env_set", map[string]any{
		"app_id": app.ID, "server_id": app.ServerID, "stack": app.StackName, "key": key,
		"secret": request.Secret, "build_time": request.BuildTime,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h appHandler) deleteEnv(w http.ResponseWriter, r *http.Request) {
	app, err := h.app(r)
	if err != nil {
		appStoreError(w, err)
		return
	}
	key := chi.URLParam(r, "key")
	if err := h.deps.Store.DeleteAppEnv(r.Context(), app.ID, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		internal(w, "delete app variable", err)
		return
	}
	h.audit(r, "app_env_deleted", map[string]any{
		"app_id": app.ID, "server_id": app.ServerID, "stack": app.StackName, "key": key,
	})
	w.WriteHeader(http.StatusNoContent)
}
