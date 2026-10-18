package i18n

import (
	"slices"
	"sort"
	"strings"
	"testing"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/text/language"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"ca", "ca"},
		{"ca_ES.UTF-8", "ca"},
		{"ca-ES-valencia", "ca"},
		{"es", "es"},
		{"es-ES", "es"},
		{"en", "en"},
		{"EN_us", "en"},
		{"fr", "en"},
		{"", "en"},
		{"C", "en"},
		{"POSIX", "en"},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := Normalize(tt.tag); got != tt.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tt.tag, got, tt.want)
			}
		})
	}
}

func TestSupported(t *testing.T) {
	want := []string{"ca", "es", "en"}
	if got := Supported(); !slices.Equal(got, want) {
		t.Fatalf("Supported() = %v, want %v", got, want)
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name string
		flag string
		env  map[string]string
		want string
	}{
		{
			name: "flag wins",
			flag: "ca",
			env:  map[string]string{"TALAIA_LANG": "es", "LC_ALL": "es_ES.UTF-8", "LANG": "en_US.UTF-8"},
			want: "ca",
		},
		{
			name: "talaia lang wins over locale",
			env:  map[string]string{"TALAIA_LANG": "es", "LC_ALL": "ca_ES.UTF-8", "LANG": "en_US.UTF-8"},
			want: "es",
		},
		{
			name: "lc_all wins over lang",
			env:  map[string]string{"LC_ALL": "ca_ES.UTF-8", "LANG": "en_US.UTF-8"},
			want: "ca",
		},
		{
			name: "lang is used",
			env:  map[string]string{"LANG": "es_ES.UTF-8"},
			want: "es",
		},
		{
			name: "C falls back to English",
			env:  map[string]string{"LANG": "C"},
			want: "en",
		},
		{
			name: "empty values are skipped",
			flag: "  ",
			env:  map[string]string{"TALAIA_LANG": "", "LC_ALL": "  ", "LANG": "ca_ES.UTF-8"},
			want: "ca",
		},
		{
			name: "nothing set falls back to English",
			env:  map[string]string{},
			want: "en",
		},
		{
			name: "unsupported language falls back to English",
			flag: "fr",
			env:  map[string]string{},
			want: "en",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			if got := Resolve(tt.flag, getenv); got != tt.want {
				t.Fatalf("Resolve(%q, env) = %q, want %q", tt.flag, got, tt.want)
			}
		})
	}
}

func TestTranslatesKnownMessages(t *testing.T) {
	localizer, err := New("ca")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := localizer.T("version.line", map[string]any{"Version": "1.2.3"}); !strings.Contains(got, "1.2.3") {
		t.Fatalf("version.line = %q, want it to contain the version", got)
	}
	if got := localizer.T("error.unknownLanguage", map[string]any{"Language": "fr"}); !strings.Contains(got, "fr") {
		t.Fatalf("error.unknownLanguage = %q, want it to contain the language", got)
	}
}

func TestUnknownIDReturnsID(t *testing.T) {
	localizer, err := New("ca")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := localizer.T("missing.id", nil); got != "missing.id" {
		t.Fatalf("T = %q, want the id itself", got)
	}
}

func TestFallsBackToEnglish(t *testing.T) {
	localizer := newTestLocalizer(t, "es", map[string]string{
		"es.toml": "[shared]\nother = \"compartido\"\n",
		"en.toml": "[shared]\nother = \"shared\"\n[onlyEnglish]\nother = \"english only\"\n",
	})

	if got := localizer.T("shared", nil); got != "compartido" {
		t.Fatalf("shared = %q, want the Spanish text", got)
	}
	if got := localizer.T("onlyEnglish", nil); got != "english only" {
		t.Fatalf("onlyEnglish = %q, want the English fallback", got)
	}
}

func newTestLocalizer(t *testing.T, lang string, files map[string]string) *Localizer {
	t.Helper()

	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	for path, content := range files {
		if _, err := bundle.ParseMessageFileBytes([]byte(content), path); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
	}

	return &Localizer{localizer: goi18n.NewLocalizer(bundle, Normalize(lang), defaultLanguage)}
}

func TestEmbeddedLocalesHaveIdenticalIDs(t *testing.T) {
	ids := make(map[string][]string, len(supportedLanguages))

	for _, lang := range supportedLanguages {
		data, err := localesFS.ReadFile(localesDir + "/" + lang + ".toml")
		if err != nil {
			t.Fatalf("read %s: %v", lang, err)
		}

		messageFile, err := goi18n.ParseMessageFileBytes(data, lang+".toml", map[string]goi18n.UnmarshalFunc{
			"toml": toml.Unmarshal,
		})
		if err != nil {
			t.Fatalf("parse %s: %v", lang, err)
		}

		langIDs := make([]string, 0, len(messageFile.Messages))
		for _, message := range messageFile.Messages {
			langIDs = append(langIDs, message.ID)
		}
		sort.Strings(langIDs)
		ids[lang] = langIDs
	}

	reference := ids["ca"]
	if len(reference) == 0 {
		t.Fatal("no messages found in the Catalan file")
	}
	for _, lang := range supportedLanguages {
		if !slices.Equal(ids[lang], reference) {
			t.Fatalf("message ids in %s = %v, want %v", lang, ids[lang], reference)
		}
	}
}
