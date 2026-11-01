package templates

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func loadTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	catalog, err := LoadFS(os.DirFS("../../.."), "templates")
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	return catalog
}

func TestTemplatesAreCompleteAndPinned(t *testing.T) {
	catalog := loadTestCatalog(t)
	templates := catalog.List()
	if len(templates) != 18 {
		t.Fatalf("templates = %d, want 18", len(templates))
	}
	seen := map[string]bool{}
	for _, template := range templates {
		if seen[template.ID] {
			t.Errorf("duplicate template %s", template.ID)
		}
		seen[template.ID] = true
		if template.Image == "" || strings.HasSuffix(template.Image, ":latest") {
			t.Errorf("%s image = %q", template.ID, template.Image)
		}
		if template.Port < 1 || template.RAMMB <= 0 {
			t.Errorf("%s port=%d ram=%d", template.ID, template.Port, template.RAMMB)
		}
		for _, language := range []string{"ca", "es", "en"} {
			if template.Name(language) == "" || template.Description(language) == "" {
				t.Errorf("%s has no %s metadata", template.ID, language)
			}
		}
	}
	for _, id := range []string{
		"uptime-kuma", "vaultwarden", "forgejo", "nextcloud", "wordpress", "ghost",
		"umami", "plausible", "paperless-ngx", "miniflux", "freshrss", "jellyfin",
		"immich", "homepage", "mealie", "postgresql", "mariadb", "redis",
	} {
		if !seen[id] {
			t.Errorf("template %s is missing", id)
		}
	}
}

func TestTemplatesDeclareFirstLoginCredentials(t *testing.T) {
	catalog := loadTestCatalog(t)
	expected := map[string]struct {
		user   string
		length int
	}{
		"miniflux":      {user: "admin", length: 32},
		"nextcloud":     {user: "admin", length: 32},
		"paperless-ngx": {user: "admin", length: 32},
		"postgresql":    {user: "talaia", length: 32},
		"mariadb":       {user: "talaia", length: 32},
		"redis":         {length: 32},
		"vaultwarden":   {length: 48},
	}
	for _, template := range catalog.List() {
		want := expected[template.ID]
		if template.CredentialUser != want.user || template.CredentialLength != want.length {
			t.Errorf("%s credential = %q/%d, want %q/%d", template.ID,
				template.CredentialUser, template.CredentialLength, want.user, want.length)
		}
		if template.CredentialLength > 0 {
			placeholder := fmt.Sprintf("{{password:%d}}", template.CredentialLength)
			if !strings.Contains(template.Compose, placeholder) {
				t.Errorf("%s has no %s placeholder", template.ID, placeholder)
			}
		}
	}
}

func TestReadTemplateCredentials(t *testing.T) {
	cases := []struct {
		name    string
		extra   []string
		user    string
		length  int
		wantErr string
	}{
		{name: "without credential"},
		{name: "user and length", extra: []string{"credential_user: admin", "credential_length: 32"}, user: "admin", length: 32},
		{name: "without user", extra: []string{"credential_length: 32"}, length: 32},
		{name: "minimum length", extra: []string{"credential_length: 8"}, length: 8},
		{name: "maximum length", extra: []string{"credential_length: 96"}, length: 96},
		{name: "maximum user", extra: []string{"credential_user: " + strings.Repeat("a", 64), "credential_length: 32"}, user: strings.Repeat("a", 64), length: 32},
		{name: "length below minimum", extra: []string{"credential_length: 7"}, wantErr: "between 8 and 96"},
		{name: "length above maximum", extra: []string{"credential_length: 97"}, wantErr: "between 8 and 96"},
		{name: "zero length", extra: []string{"credential_length: 0"}, wantErr: "between 8 and 96"},
		{name: "non numeric length", extra: []string{"credential_length: many"}, wantErr: "must be an integer"},
		{name: "user without length", extra: []string{"credential_user: admin"}, wantErr: "requires credential_length"},
		{name: "user too long", extra: []string{"credential_user: " + strings.Repeat("a", 65), "credential_length: 32"}, wantErr: "at most 64"},
		{name: "user with control character", extra: []string{`credential_user: "ad\x01min"`, "credential_length: 32"}, wantErr: "control characters"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			content := testTemplateFile(test.extra...)
			fsys := fstest.MapFS{"test/template.yaml": &fstest.MapFile{Data: []byte(content)}}
			template, err := readTemplate(fsys, "test/template.yaml", "test")
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if template.CredentialUser != test.user || template.CredentialLength != test.length {
				t.Errorf("credential = %q/%d, want %q/%d",
					template.CredentialUser, template.CredentialLength, test.user, test.length)
			}
		})
	}
}

func testTemplateFile(extra ...string) string {
	lines := []string{
		"id: test",
		`version: "1.0.0"`,
		"name_en: Test",
		"name_es: Prueba",
		"name_ca: Prova",
		"description_en: Test template.",
		"description_es: Plantilla de prueba.",
		"description_ca: Plantilla de prova.",
		"image: example/test:1.0.0",
		"service: app",
		"port: 8080",
		"ram_mb: 128",
	}
	lines = append(lines, extra...)
	lines = append(lines, "compose: |", "  services:", "    app:", "      image: example/test:1.0.0")
	return strings.Join(lines, "\n") + "\n"
}

func TestTemplateRendersDomainPasswordAndHostPort(t *testing.T) {
	catalog := loadTestCatalog(t)
	template, ok := catalog.Get("vaultwarden")
	if !ok {
		t.Fatal("vaultwarden template is missing")
	}
	password := strings.Repeat("p", 64)
	rendered, err := template.Render(RenderValues{
		Domain: "vault.example.com", Password: password, HostPort: 23456,
		Stack: "vault", AppID: "app-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"https://vault.example.com", "127.0.0.1:23456:80", strings.Repeat("p", 48)} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered template lacks %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "{{") {
		t.Errorf("unrendered placeholder:\n%s", rendered)
	}
}

func TestEveryTemplateRenders(t *testing.T) {
	catalog := loadTestCatalog(t)
	for _, template := range catalog.List() {
		rendered, err := template.Render(RenderValues{
			Domain: "app.example.com", Password: strings.Repeat("x", 64), HostPort: 23456,
			Stack: "app", AppID: "app-1",
		})
		if err != nil {
			t.Errorf("%s: %v", template.ID, err)
			continue
		}
		if !strings.Contains(rendered, "services:") || strings.Contains(rendered, "{{") {
			t.Errorf("%s rendered badly:\n%s", template.ID, rendered)
		}
	}
}

func TestTemplatesPassDockerComposeConfig(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	check := exec.Command("docker", "compose", "version")
	if err := check.Run(); err != nil {
		t.Skip("docker compose is not installed")
	}
	catalog := loadTestCatalog(t)
	for _, template := range catalog.List() {
		t.Run(template.ID, func(t *testing.T) {
			rendered, err := template.Render(RenderValues{
				Domain: "app.example.com", Password: strings.Repeat("x", 64), HostPort: 23456,
				Stack: "app", AppID: "app-1",
			})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			file := filepath.Join(dir, "compose.yaml")
			if err := os.WriteFile(file, []byte(rendered), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "docker", "compose", "-f", file, "config", "--quiet")
			command.Dir = dir
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("docker compose config: %v\n%s", err, output)
			}
		})
	}
}
