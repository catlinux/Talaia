package templates

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
