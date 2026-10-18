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

func TestTranslatesKnownMessages(t *testing.T) {
	localizer, err := New("ca")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := localizer.T("app.name", nil); got != "Talaia" {
		t.Fatalf("app.name = %q, want Talaia", got)
	}
	if got := localizer.T("app.starting", map[string]any{"Version": "1.2.3"}); !strings.Contains(got, "1.2.3") {
		t.Fatalf("app.starting = %q, want it to contain the version", got)
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
	localizer := newTestLocalizer(t, "ca", map[string]string{
		"ca.toml": "[shared]\nother = \"compartit\"\n",
		"en.toml": "[shared]\nother = \"shared\"\n[onlyEnglish]\nother = \"english only\"\n",
	})

	if got := localizer.T("shared", nil); got != "compartit" {
		t.Fatalf("shared = %q, want the Catalan text", got)
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
