package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrLastAdmin is returned when an operation would leave the installation
// without an active organization administrator.
var ErrLastAdmin = errors.New("at least one active organization admin is required")

// ErrGroupNameExists is returned when a server group name is already in use.
var ErrGroupNameExists = errors.New("server group name already exists")

// ErrTeamNameExists is returned when a team name is already in use.
var ErrTeamNameExists = errors.New("team name already exists")

// ErrBindingExists is returned when the same subject already has a role on the
// same scope.
var ErrBindingExists = errors.New("role binding already exists")

// Subject types of a role binding.
const (
	SubjectUser   = "user"
	SubjectTeam   = "team"
	SubjectAPIKey = "api_key"
)

// Scope types of a role binding.
const (
	ScopeOrganization = "organization"
	ScopeGroup        = "group"
	ScopeServer       = "server"
	ScopeStack        = "stack"
	ScopeContainer    = "container"
)

// Roles fixed for phase 4.
const (
	RoleAdmin          = "admin"
	RoleServerOperator = "server_operator"
	RoleAppOperator    = "app_operator"
	RoleViewer         = "viewer"
	RoleMetricsOnly    = "metrics_only"
)

// lastAdminLockKey serialises every operation that can remove an organization
// administrator. It is only used inside a transaction.
const lastAdminLockKey int64 = 0x74616c616961

// ServerGroup groups servers for permissions and for the panel list.
type ServerGroup struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// Group is the short form used by callers that already work with groups.
type Group = ServerGroup

// Team is a named set of users that can receive role bindings.
type Team struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// TeamMember is one membership row enriched with the user's display fields.
type TeamMember struct {
	TeamID      string
	UserID      string
	Username    string
	DisplayName string
	Email       string
	CreatedAt   time.Time
}

// RoleBinding grants one role to a user, a team or an API key on one scope.
// Empty scope fields mean NULL and therefore do not apply to that scope.
type RoleBinding struct {
	ID            string
	SubjectType   string
	SubjectID     string
	ScopeType     string
	ScopeServerID string
	ScopeGroupID  string
	ScopeName     string
	Role          string
	CreatedBy     string
	CreatedAt     time.Time
}

func validRole(role string) bool {
	switch role {
	case RoleAdmin, RoleServerOperator, RoleAppOperator, RoleViewer, RoleMetricsOnly:
		return true
	default:
		return false
	}
}

func validSubjectType(subjectType string) bool {
	switch subjectType {
	case SubjectUser, SubjectTeam, SubjectAPIKey:
		return true
	default:
		return false
	}
}

func validScopeType(scopeType string) bool {
	switch scopeType {
	case ScopeOrganization, ScopeGroup, ScopeServer, ScopeStack, ScopeContainer:
		return true
	default:
		return false
	}
}

// validateBinding applies the same coherence rules as the database CHECK, so
// callers get a useful error before a round trip.
func validateBinding(subjectType, subjectID, scopeType, serverID, groupID, scopeName, role string) error {
	if !validSubjectType(subjectType) {
		return fmt.Errorf("invalid subject type %q", subjectType)
	}
	if strings.TrimSpace(subjectID) == "" {
		return errors.New("subject id is required")
	}
	if !validScopeType(scopeType) {
		return fmt.Errorf("invalid scope type %q", scopeType)
	}
	if !validRole(role) {
		return fmt.Errorf("invalid role %q", role)
	}

	serverID = strings.TrimSpace(serverID)
	groupID = strings.TrimSpace(groupID)
	scopeName = strings.TrimSpace(scopeName)
	switch scopeType {
	case ScopeOrganization:
		if serverID != "" || groupID != "" || scopeName != "" {
			return errors.New("organization scope cannot have ids or a name")
		}
	case ScopeGroup:
		if serverID != "" || groupID == "" || scopeName != "" {
			return errors.New("group scope needs only a group id")
		}
	case ScopeServer:
		if serverID == "" || groupID != "" || scopeName != "" {
			return errors.New("server scope needs only a server id")
		}
	case ScopeStack, ScopeContainer:
		if serverID == "" || groupID != "" || scopeName == "" {
			return errors.New("stack and container scopes need a server id and a name")
		}
	}
	return nil
}

func scanGroup(row rowScanner) (ServerGroup, error) {
	var group ServerGroup
	err := row.Scan(&group.ID, &group.Name, &group.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServerGroup{}, ErrNotFound
	}
	return group, err
}

// CreateGroup inserts a server group.
func (s *Store) CreateGroup(ctx context.Context, name string) (ServerGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ServerGroup{}, errors.New("server group name is required")
	}
	group, err := scanGroup(s.pool.QueryRow(ctx,
		`INSERT INTO server_group (name) VALUES ($1) RETURNING id, name, created_at`, name))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ServerGroup{}, ErrGroupNameExists
		}
		return ServerGroup{}, fmt.Errorf("create server group %q: %w", name, err)
	}
	return group, nil
}


// GetGroup returns a server group by id.
func (s *Store) GetGroup(ctx context.Context, id string) (ServerGroup, error) {
	group, err := scanGroup(s.pool.QueryRow(ctx,
		`SELECT id, name, created_at FROM server_group WHERE id = $1`, id))
	if errors.Is(err, ErrNotFound) {
		return ServerGroup{}, ErrNotFound
	}
	if err != nil {
		return ServerGroup{}, fmt.Errorf("get server group %q: %w", id, err)
	}
	return group, nil
}

// ListGroups returns all server groups ordered by name.
func (s *Store) ListGroups(ctx context.Context) ([]ServerGroup, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, created_at FROM server_group ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("list server groups: %w", err)
	}
	defer rows.Close()

	groups := make([]ServerGroup, 0, 8)
	for rows.Next() {
		group, err := scanGroup(rows)
		if err != nil {
			return nil, fmt.Errorf("scan server group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read server groups: %w", err)
	}
	return groups, nil
}

// UpdateGroup renames a server group.
func (s *Store) UpdateGroup(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("server group name is required")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE server_group SET name = $2 WHERE id = $1`, id, name)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ErrGroupNameExists
		}
		return fmt.Errorf("update server group %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteGroup removes a server group. Servers in it become ungrouped and
// group-scoped role bindings are removed by the foreign key.
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM server_group WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete server group %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanTeam(row rowScanner) (Team, error) {
	var team Team
	err := row.Scan(&team.ID, &team.Name, &team.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Team{}, ErrNotFound
	}
	return team, err
}

// CreateTeam inserts a team.
func (s *Store) CreateTeam(ctx context.Context, name string) (Team, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Team{}, errors.New("team name is required")
	}
	team, err := scanTeam(s.pool.QueryRow(ctx,
		`INSERT INTO team (name) VALUES ($1) RETURNING id, name, created_at`, name))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return Team{}, ErrTeamNameExists
		}
		return Team{}, fmt.Errorf("create team %q: %w", name, err)
	}
	return team, nil
}

// GetTeam returns a team by id.
func (s *Store) GetTeam(ctx context.Context, id string) (Team, error) {
	team, err := scanTeam(s.pool.QueryRow(ctx,
		`SELECT id, name, created_at FROM team WHERE id = $1`, id))
	if errors.Is(err, ErrNotFound) {
		return Team{}, ErrNotFound
	}
	if err != nil {
		return Team{}, fmt.Errorf("get team %q: %w", id, err)
	}
	return team, nil
}

// ListTeams returns all teams ordered by name.
func (s *Store) ListTeams(ctx context.Context) ([]Team, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, created_at FROM team ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()

	teams := make([]Team, 0, 8)
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		teams = append(teams, team)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read teams: %w", err)
	}
	return teams, nil
}

// UpdateTeam renames a team.
func (s *Store) UpdateTeam(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("team name is required")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE team SET name = $2 WHERE id = $1`, id, name)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ErrTeamNameExists
		}
		return fmt.Errorf("update team %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AddTeamMember adds a user to a team. Adding an existing membership is a no-op.
func (s *Store) AddTeamMember(ctx context.Context, teamID, userID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO team_member (team_id, user_id) VALUES ($1, $2)
		 ON CONFLICT (team_id, user_id) DO NOTHING`, teamID, userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrNotFound
		}
		return fmt.Errorf("add team member: %w", err)
	}
	return nil
}

// ListTeamMembers returns the members of a team with their user fields.
func (s *Store) ListTeamMembers(ctx context.Context, teamID string) ([]TeamMember, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT tm.team_id, tm.user_id, u.username, u.display_name, COALESCE(u.email, ''), tm.created_at
		 FROM team_member tm
		 JOIN app_user u ON u.id = tm.user_id
		 WHERE tm.team_id = $1
		 ORDER BY u.username, u.id`, teamID)
	if err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	defer rows.Close()

	members := make([]TeamMember, 0, 8)
	for rows.Next() {
		var member TeamMember
		if err := rows.Scan(&member.TeamID, &member.UserID, &member.Username,
			&member.DisplayName, &member.Email, &member.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan team member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read team members: %w", err)
	}
	return members, nil
}

// ListTeamMemberIDs returns just the user ids of a team.
func (s *Store) ListTeamMemberIDs(ctx context.Context, teamID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id FROM team_member WHERE team_id = $1 ORDER BY user_id`, teamID)
	if err != nil {
		return nil, fmt.Errorf("list team member ids: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan team member id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read team member ids: %w", err)
	}
	return ids, nil
}

func scanRoleBinding(row rowScanner) (RoleBinding, error) {
	var binding RoleBinding
	err := row.Scan(&binding.ID, &binding.SubjectType, &binding.SubjectID, &binding.ScopeType,
		&binding.ScopeServerID, &binding.ScopeGroupID, &binding.ScopeName,
		&binding.Role, &binding.CreatedBy, &binding.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return RoleBinding{}, ErrNotFound
	}
	if err != nil {
		return RoleBinding{}, err
	}
	return binding, nil
}

const roleBindingColumns = `id, subject_type, subject_id, scope_type,
	COALESCE(scope_server_id::text, ''), COALESCE(scope_group_id::text, ''),
	COALESCE(scope_name, ''), role, created_by, created_at`

// CreateRoleBinding adds a binding. The database unique index prevents the
// same subject and scope from having two roles.
func (s *Store) CreateRoleBinding(ctx context.Context, binding RoleBinding) (RoleBinding, error) {
	binding.SubjectType = strings.TrimSpace(binding.SubjectType)
	binding.SubjectID = strings.TrimSpace(binding.SubjectID)
	binding.ScopeType = strings.TrimSpace(binding.ScopeType)
	binding.ScopeServerID = strings.TrimSpace(binding.ScopeServerID)
	binding.ScopeGroupID = strings.TrimSpace(binding.ScopeGroupID)
	binding.ScopeName = strings.TrimSpace(binding.ScopeName)
	binding.Role = strings.TrimSpace(binding.Role)
	binding.CreatedBy = strings.TrimSpace(binding.CreatedBy)

	if err := validateBinding(binding.SubjectType, binding.SubjectID, binding.ScopeType,
		binding.ScopeServerID, binding.ScopeGroupID, binding.ScopeName, binding.Role); err != nil {
		return RoleBinding{}, err
	}

	created, err := scanRoleBinding(s.pool.QueryRow(ctx,
		`INSERT INTO role_binding
			(subject_type, subject_id, scope_type, scope_server_id, scope_group_id, scope_name, role, created_by)
		 VALUES ($1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, NULLIF($6, ''), $7, $8)
		 RETURNING `+roleBindingColumns,
		binding.SubjectType, binding.SubjectID, binding.ScopeType, binding.ScopeServerID,
		binding.ScopeGroupID, binding.ScopeName, binding.Role, binding.CreatedBy))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return RoleBinding{}, ErrBindingExists
		}
		return RoleBinding{}, fmt.Errorf("create role binding: %w", err)
	}
	return created, nil
}

// ListRoleBindings returns the bindings of one subject.
func (s *Store) ListRoleBindings(ctx context.Context, subjectType, subjectID string) ([]RoleBinding, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+roleBindingColumns+`
		 FROM role_binding
		 WHERE subject_type = $1 AND subject_id = $2
		 ORDER BY created_at, id`, subjectType, subjectID)
	if err != nil {
		return nil, fmt.Errorf("list role bindings: %w", err)
	}
	defer rows.Close()

	bindings := make([]RoleBinding, 0, 8)
	for rows.Next() {
		binding, err := scanRoleBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan role binding: %w", err)
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read role bindings: %w", err)
	}
	return bindings, nil
}

// ListRoleBindingsForUser returns direct bindings plus the bindings of every
// team the user belongs to. The API converts these rows into rbac.Binding.
func (s *Store) ListRoleBindingsForUser(ctx context.Context, userID string) ([]RoleBinding, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+roleBindingColumns+`
		 FROM role_binding
		 WHERE (subject_type = 'user' AND subject_id = $1)
		    OR (subject_type = 'team' AND subject_id IN (
		        SELECT team_id FROM team_member WHERE user_id = $1))
		 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list role bindings for user: %w", err)
	}
	defer rows.Close()

	bindings := make([]RoleBinding, 0, 8)
	for rows.Next() {
		binding, err := scanRoleBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan role binding for user: %w", err)
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read role bindings for user: %w", err)
	}
	return bindings, nil
}


// ListAllRoleBindings returns every binding, for the permissions screen.
func (s *Store) ListAllRoleBindings(ctx context.Context) ([]RoleBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+roleBindingColumns+` FROM role_binding ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list all role bindings: %w", err)
	}
	defer rows.Close()

	bindings := make([]RoleBinding, 0, 16)
	for rows.Next() {
		binding, err := scanRoleBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan role binding: %w", err)
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read all role bindings: %w", err)
	}
	return bindings, nil
}

func bindingAffectsAdmins(binding RoleBinding) bool {
	return binding.Role == RoleAdmin && binding.ScopeType == ScopeOrganization &&
		(binding.SubjectType == SubjectUser || binding.SubjectType == SubjectTeam)
}

// DeleteRoleBinding removes a binding and refuses to remove the last active
// organization administrator.
func (s *Store) DeleteRoleBinding(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete role binding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockLastAdmin(ctx, tx); err != nil {
		return err
	}

	binding, err := scanRoleBinding(tx.QueryRow(ctx,
		`SELECT `+roleBindingColumns+` FROM role_binding WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock role binding %q: %w", id, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM role_binding WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete role binding %q: %w", id, err)
	}
	if bindingAffectsAdmins(binding) {
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete role binding: %w", err)
	}
	return nil
}

// UpdateRoleBindingRole changes the role of a binding. It refuses to demote the
// last active organization administrator.
func (s *Store) UpdateRoleBindingRole(ctx context.Context, id, role string) error {
	role = strings.TrimSpace(role)
	if !validRole(role) {
		return fmt.Errorf("invalid role %q", role)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin update role binding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockLastAdmin(ctx, tx); err != nil {
		return err
	}

	binding, err := scanRoleBinding(tx.QueryRow(ctx,
		`SELECT `+roleBindingColumns+` FROM role_binding WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock role binding %q: %w", id, err)
	}
	if binding.Role == role {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit role binding: %w", err)
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE role_binding SET role = $2 WHERE id = $1`, id, role); err != nil {
		return fmt.Errorf("update role binding %q: %w", id, err)
	}
	if bindingAffectsAdmins(binding) || (role == RoleAdmin && binding.ScopeType == ScopeOrganization &&
		(binding.SubjectType == SubjectUser || binding.SubjectType == SubjectTeam)) {
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit role binding: %w", err)
	}
	return nil
}




// RemoveTeamMember removes a user from a team and enforces the last-admin
// invariant when the team carries the only organization admin binding.
func (s *Store) RemoveTeamMember(ctx context.Context, teamID, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin remove team member: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockLastAdmin(ctx, tx); err != nil {
		return err
	}

	var hadAdmin bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM role_binding
			WHERE subject_type = 'team' AND subject_id = $1
			  AND scope_type = 'organization' AND role = 'admin'
		)`, teamID).Scan(&hadAdmin); err != nil {
		return fmt.Errorf("check team admin binding: %w", err)
	}

	tag, err := tx.Exec(ctx,
		`DELETE FROM team_member WHERE team_id = $1 AND user_id = $2`, teamID, userID)
	if err != nil {
		return fmt.Errorf("remove team member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if hadAdmin {
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit remove team member: %w", err)
	}
	return nil
}

// DeleteTeam removes a team, its memberships and its role bindings. It refuses
// to delete the team that carries the last active organization admin.
func (s *Store) DeleteTeam(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete team: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockLastAdmin(ctx, tx); err != nil {
		return err
	}

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM team WHERE id = $1)`, id).Scan(&exists); err != nil {
		return fmt.Errorf("check team: %w", err)
	}
	if !exists {
		return ErrNotFound
	}

	var hadAdmin bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM role_binding
			WHERE subject_type = 'team' AND subject_id = $1
			  AND scope_type = 'organization' AND role = 'admin'
		)`, id).Scan(&hadAdmin); err != nil {
		return fmt.Errorf("check team admin binding: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM role_binding WHERE subject_type = 'team' AND subject_id = $1`, id); err != nil {
		return fmt.Errorf("delete team bindings: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM team_member WHERE team_id = $1`, id); err != nil {
		return fmt.Errorf("delete team memberships: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM team WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete team: %w", err)
	}
	if hadAdmin {
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete team: %w", err)
	}
	return nil
}

// SetServerGroup puts a server in a group, or removes it from one when groupID
// is empty.
func (s *Store) SetServerGroup(ctx context.Context, serverID, groupID string) error {
	groupID = strings.TrimSpace(groupID)
	tag, err := s.pool.Exec(ctx,
		`UPDATE server SET group_id = NULLIF($2, '')::uuid WHERE id = $1`, serverID, groupID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrNotFound
		}
		return fmt.Errorf("set server group for %q: %w", serverID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountActiveOrganizationAdmins returns the number of non-disabled users with
// the organization admin role, directly or through a team.
func (s *Store) CountActiveOrganizationAdmins(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, activeAdminCountQuery).Scan(&count); err != nil {
		return 0, fmt.Errorf("count organization admins: %w", err)
	}
	return count, nil
}

const activeAdminCountQuery = `
	SELECT count(*)
	FROM app_user u
	WHERE u.disabled_at IS NULL
	  AND (
	    EXISTS (
	      SELECT 1 FROM role_binding b
	      WHERE b.subject_type = 'user' AND b.subject_id = u.id
	        AND b.scope_type = 'organization' AND b.role = 'admin'
	    )
	    OR EXISTS (
	      SELECT 1 FROM team_member tm
	      JOIN role_binding b ON b.subject_type = 'team' AND b.subject_id = tm.team_id
	      WHERE tm.user_id = u.id
	        AND b.scope_type = 'organization' AND b.role = 'admin'
	    )
	  )`

// lockLastAdmin serialises admin-affecting transactions. An advisory lock is
// cheaper and safer than trying to lock a set of rows that does not exist yet.
func lockLastAdmin(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lastAdminLockKey); err != nil {
		return fmt.Errorf("lock last admin: %w", err)
	}
	return nil
}

func countActiveAdmins(ctx context.Context, tx pgx.Tx) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, activeAdminCountQuery).Scan(&count); err != nil {
		return 0, fmt.Errorf("count organization admins: %w", err)
	}
	return count, nil
}

func ensureAdminRemains(ctx context.Context, tx pgx.Tx) error {
	count, err := countActiveAdmins(ctx, tx)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrLastAdmin
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

func validateScope(scopeType, serverID, groupID, name string) error {
	return validateBinding(SubjectUser, "00000000-0000-0000-0000-000000000000",
		scopeType, serverID, groupID, name, RoleViewer)
}

func scopeValues(scopeType, serverID, groupID, name string) (any, any, any) {
	switch scopeType {
	case ScopeGroup:
		return nil, groupID, nil
	case ScopeServer:
		return serverID, nil, nil
	case ScopeStack, ScopeContainer:
		return serverID, nil, name
	default:
		return nil, nil, nil
	}
}

// ErrRoleBindingExists is an explicit alias for ErrBindingExists.
var ErrRoleBindingExists = ErrBindingExists
