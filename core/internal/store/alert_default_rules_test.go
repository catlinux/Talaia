package store

import (
	"os"
	"strings"
	"testing"
)

func TestAlertDefaultRulesMigrationIsIdempotentAndRespectsExistingRules(t *testing.T) {
	s, ctx := alertTestStore(t)
	raw, err := os.ReadFile("migrations/0016_alert_default_rules.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(raw)
	upStart := strings.Index(text, "-- +goose Up")
	downStart := strings.Index(text, "-- +goose Down")
	if upStart < 0 || downStart <= upStart {
		t.Fatal("migration does not have the expected goose sections")
	}
	up := strings.ReplaceAll(text[upStart:downStart], "-- +goose Up", "")
	statements := make([]string, 0, 3)
	for _, statement := range strings.Split(up, ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			statements = append(statements, statement)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	runUp := func() {
		t.Helper()
		for _, statement := range statements {
			if _, err := tx.Exec(ctx, statement); err != nil {
				t.Fatalf("run migration statement: %v", err)
			}
		}
	}
	count := func(typ, severity string) int {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM alert_rule WHERE type=$1 AND severity=$2`, typ, severity).Scan(&n); err != nil {
			t.Fatalf("count rules: %v", err)
		}
		return n
	}
	namedCount := func(name string) int {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM alert_rule WHERE name=$1 AND builtin AND scope_type='organization'`, name).Scan(&n); err != nil {
			t.Fatalf("count named rule: %v", err)
		}
		return n
	}
	allNamedCount := func(name string) int {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM alert_rule WHERE name=$1`, name).Scan(&n); err != nil {
			t.Fatalf("count rule by name: %v", err)
		}
		return n
	}

	// The shared test database may already contain rules of these pairs. Remove
	// them inside this transaction so the first Up is deterministic.
	if _, err := tx.Exec(ctx, `DELETE FROM alert_rule WHERE (type, severity) IN (
		('cpu_high', 'warning'), ('mem_high', 'warning'), ('disk_high', 'warning'))`); err != nil {
		t.Fatalf("clear target rules: %v", err)
	}

	runUp()
	if got := count("cpu_high", "warning"); got != 1 {
		t.Fatalf("cpu_high warning count=%d, want 1", got)
	}
	if got := count("mem_high", "warning"); got != 1 {
		t.Fatalf("mem_high warning count=%d, want 1", got)
	}
	if got := count("disk_high", "warning"); got != 1 {
		t.Fatalf("disk_high warning count=%d, want 1", got)
	}
	for _, name := range []string{"CPU usage high", "Memory usage high", "Disk usage warning"} {
		if got := namedCount(name); got != 1 {
			t.Fatalf("%q built-in count=%d, want 1", name, got)
		}
	}

	// Running the Up section again must not duplicate the defaults.
	runUp()
	if got := count("cpu_high", "warning") + count("mem_high", "warning") + count("disk_high", "warning"); got != 3 {
		t.Fatalf("default rule total after second Up=%d, want 3", got)
	}

	// A user rule with the same type and severity blocks the default insert.
	if _, err := tx.Exec(ctx, `DELETE FROM alert_rule WHERE name='CPU usage high' AND builtin`); err != nil {
		t.Fatalf("delete CPU default: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO alert_rule (name, type, severity, params, scope_type, builtin)
		VALUES ('Custom CPU warning', 'cpu_high', 'warning', '{"threshold":70,"for_seconds":30}'::jsonb, 'organization', false)`); err != nil {
		t.Fatalf("insert custom rule: %v", err)
	}
	runUp()
	if got := namedCount("CPU usage high"); got != 0 {
		t.Fatalf("CPU default inserted despite an existing rule: count=%d", got)
	}
	if got := allNamedCount("Custom CPU warning"); got != 1 {
		t.Fatalf("custom rule count=%d, want 1", got)
	}
	if got := count("cpu_high", "warning"); got != 1 {
		t.Fatalf("cpu_high warning count=%d, want only the custom rule", got)
	}
}
