// Package templates loads and renders the one-click app templates bundled with
// Talaia. A template is a small YAML file with localized metadata and a Compose
// file that uses {{domain}}, {{password:N}} and the other placeholders.
package templates

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	templateIDRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	imageRe        = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*:[a-zA-Z0-9._-]+$`)
	serviceRe      = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	placeholderRe  = regexp.MustCompile(`\{\{([^{}]+)\}\}`)
	passwordRe     = regexp.MustCompile(`^password:(\d+)$`)
	requiredLocals = []string{"ca", "es", "en"}
	knownKeys      = map[string]bool{
		"id": true, "version": true,
		"name_ca": true, "name_es": true, "name_en": true,
		"description_ca": true, "description_es": true, "description_en": true,
		"image": true, "service": true, "port": true, "ram_mb": true, "compose": true,
		"credential_user": true, "credential_length": true,
	}
)

// Template is one one-click app.
type Template struct {
	ID           string
	Version      string
	Names        map[string]string
	Descriptions map[string]string
	Image        string
	Service      string
	Port         int
	RAMMB        int
	Compose      string
	// CredentialUser and CredentialLength describe the first-login credential
	// when the template renders a password the user must type. A zero length
	// means the template has no first-login credential.
	CredentialUser   string
	CredentialLength int
}

// Catalog is the set of templates available to the core.
type Catalog struct {
	byID map[string]Template
}

// RenderValues are the values substituted into a template.
type RenderValues struct {
	Domain   string
	Password string
	HostPort int
	Stack    string
	AppID    string
}

// LoadDir loads every template under dir.
func LoadDir(dir string) (*Catalog, error) {
	return load(os.DirFS(dir), ".")
}

// LoadFS loads every template under root in fsys.
func LoadFS(fsys fs.FS, root string) (*Catalog, error) {
	return load(fsys, root)
}

func load(fsys fs.FS, root string) (*Catalog, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("read templates: %w", err)
	}
	catalog := &Catalog{byID: map[string]Template{}}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		file := path.Join(root, entry.Name(), "template.yaml")
		template, err := readTemplate(fsys, file, entry.Name())
		if err != nil {
			return nil, err
		}
		if _, exists := catalog.byID[template.ID]; exists {
			return nil, fmt.Errorf("duplicate template id %q", template.ID)
		}
		catalog.byID[template.ID] = template
	}
	if len(catalog.byID) == 0 {
		return nil, errors.New("no templates found")
	}
	return catalog, nil
}

// List returns the templates ordered by id.
func (c *Catalog) List() []Template {
	result := make([]Template, 0, len(c.byID))
	for _, template := range c.byID {
		result = append(result, template)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// Get returns one template by id.
func (c *Catalog) Get(id string) (Template, bool) {
	if c == nil {
		return Template{}, false
	}
	template, ok := c.byID[id]
	return template, ok
}

// Name returns the localized name, falling back to English.
func (t Template) Name(language string) string {
	if value := t.Names[language]; value != "" {
		return value
	}
	return t.Names["en"]
}

// Description returns the localized description, falling back to English.
func (t Template) Description(language string) string {
	if value := t.Descriptions[language]; value != "" {
		return value
	}
	return t.Descriptions["en"]
}

// Render replaces the placeholders of the template. A password placeholder uses
// the first N characters of Values.Password.
func (t Template) Render(values RenderValues) (string, error) {
	if values.Domain == "" && strings.Contains(t.Compose, "{{domain}}") {
		return "", errors.New("a domain is required")
	}
	if values.Password == "" && strings.Contains(t.Compose, "{{password:") {
		return "", errors.New("a password is required")
	}
	if values.HostPort == 0 && strings.Contains(t.Compose, "{{host_port}}") {
		return "", errors.New("a host port is required")
	}
	port := t.Port
	rendered := placeholderRe.ReplaceAllStringFunc(t.Compose, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}")
		switch name {
		case "domain":
			return values.Domain
		case "image":
			return t.Image
		case "port":
			return strconv.Itoa(port)
		case "service":
			return t.Service
		case "host_port":
			return strconv.Itoa(values.HostPort)
		case "stack":
			return values.Stack
		case "app_id":
			return values.AppID
		}
		if parts := passwordRe.FindStringSubmatch(name); parts != nil {
			length, _ := strconv.Atoi(parts[1])
			if len(values.Password) < length {
				return values.Password
			}
			return values.Password[:length]
		}
		return match
	})
	if strings.Contains(rendered, "{{") {
		return "", errors.New("the template has an unknown placeholder")
	}
	return rendered, nil
}

func readTemplate(fsys fs.FS, file, directory string) (Template, error) {
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return Template{}, fmt.Errorf("read %s: %w", file, err)
	}
	values := map[string]string{}
	var compose []string
	inCompose := false
	for index, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if inCompose {
			if line == "" {
				compose = append(compose, "")
				continue
			}
			if !strings.HasPrefix(line, "  ") {
				return Template{}, fmt.Errorf("%s:%d: compose lines must be indented", file, index+1)
			}
			compose = append(compose, line[2:])
			continue
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return Template{}, fmt.Errorf("%s:%d: expected key: value", file, index+1)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "compose" && value == "|" {
			inCompose = true
			continue
		}
		if !knownKeys[key] {
			return Template{}, fmt.Errorf("%s:%d: unknown key %q", file, index+1, key)
		}
		if _, exists := values[key]; exists {
			return Template{}, fmt.Errorf("%s:%d: duplicate key %q", file, index+1, key)
		}
		values[key] = unquote(value)
	}
	template := Template{
		ID: values["id"], Version: values["version"], Image: values["image"],
		Service: values["service"], Names: map[string]string{}, Descriptions: map[string]string{},
		Compose: strings.Join(compose, "\n"),
	}
	template.Port, _ = strconv.Atoi(values["port"])
	template.RAMMB, _ = strconv.Atoi(values["ram_mb"])
	template.CredentialUser = values["credential_user"]
	if raw, ok := values["credential_length"]; ok {
		length, err := strconv.Atoi(raw)
		if err != nil {
			return Template{}, fmt.Errorf("%s: credential_length must be an integer", file)
		}
		if length == 0 {
			return Template{}, fmt.Errorf("%s: credential_length must be between 8 and 96", file)
		}
		template.CredentialLength = length
	}
	for _, language := range requiredLocals {
		template.Names[language] = values["name_"+language]
		template.Descriptions[language] = values["description_"+language]
	}
	if err := template.validate(directory); err != nil {
		return Template{}, fmt.Errorf("%s: %w", file, err)
	}
	return template, nil
}

func (t Template) validate(directory string) error {
	if !templateIDRe.MatchString(t.ID) || t.ID != directory {
		return fmt.Errorf("invalid template id %q", t.ID)
	}
	if strings.TrimSpace(t.Version) == "" {
		return errors.New("version is required")
	}
	for _, language := range requiredLocals {
		if strings.TrimSpace(t.Names[language]) == "" || strings.TrimSpace(t.Descriptions[language]) == "" {
			return fmt.Errorf("name_%s and description_%s are required", language, language)
		}
	}
	if !imageRe.MatchString(t.Image) || strings.HasSuffix(t.Image, ":latest") {
		return fmt.Errorf("image %q is not pinned", t.Image)
	}
	if !serviceRe.MatchString(t.Service) {
		return fmt.Errorf("invalid service %q", t.Service)
	}
	if t.Port < 1 || t.Port > 65535 {
		return fmt.Errorf("invalid port %d", t.Port)
	}
	if t.RAMMB <= 0 {
		return errors.New("ram_mb must be positive")
	}
	if t.CredentialLength != 0 && (t.CredentialLength < 8 || t.CredentialLength > 96) {
		return fmt.Errorf("credential_length must be between 8 and 96")
	}
	if utf8.RuneCountInString(t.CredentialUser) > 64 {
		return errors.New("credential_user must be at most 64 characters")
	}
	if strings.IndexFunc(t.CredentialUser, unicode.IsControl) >= 0 {
		return errors.New("credential_user must not contain control characters")
	}
	if t.CredentialUser != "" && t.CredentialLength == 0 {
		return errors.New("credential_user requires credential_length")
	}
	if !strings.Contains(t.Compose, "services:") {
		return errors.New("compose has no services")
	}
	for _, line := range strings.Split(t.Compose, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "image:") {
			ref := strings.TrimSpace(strings.TrimPrefix(line, "image:"))
			if ref == "" || strings.HasSuffix(ref, ":latest") {
				return fmt.Errorf("compose image %q is not pinned", ref)
			}
		}
	}
	for _, match := range placeholderRe.FindAllStringSubmatch(t.Compose, -1) {
		name := match[1]
		switch name {
		case "domain", "image", "port", "service", "host_port", "stack", "app_id":
			continue
		}
		parts := passwordRe.FindStringSubmatch(name)
		if parts == nil {
			return fmt.Errorf("unknown placeholder %q", name)
		}
		length, _ := strconv.Atoi(parts[1])
		if length < 8 || length > 128 {
			return fmt.Errorf("invalid password length %d", length)
		}
	}
	return nil
}

func unquote(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	return value
}
