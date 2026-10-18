// Package i18n provides the server-side translations of talaia-core.
package i18n

import (
	"embed"
	"fmt"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/text/language"
)

//go:embed locales/*.toml
var localesFS embed.FS

const (
	defaultLanguage = "en"
	localesDir      = "locales"
)

// supportedLanguages are the languages the messages are written in. Catalan is
// the reference language.
var supportedLanguages = []string{"ca", "es", "en"}

// Localizer translates message ids, falling back to English.
type Localizer struct {
	localizer *goi18n.Localizer
}

// Supported returns the languages the server-side messages are written in.
func Supported() []string {
	return append([]string(nil), supportedLanguages...)
}

// New builds a Localizer for lang. Unknown languages use English.
func New(lang string) (*Localizer, error) {
	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	for _, lang := range supportedLanguages {
		if _, err := bundle.LoadMessageFileFS(localesFS, localesDir+"/"+lang+".toml"); err != nil {
			return nil, fmt.Errorf("load %s messages: %w", lang, err)
		}
	}

	return &Localizer{
		localizer: goi18n.NewLocalizer(bundle, Normalize(lang), defaultLanguage),
	}, nil
}

// T returns the message id in the localizer language. It falls back to English
// and, when the id is unknown everywhere, returns the id itself. It never
// panics.
func (l *Localizer) T(id string, data map[string]any) string {
	message, err := l.localizer.Localize(&goi18n.LocalizeConfig{
		MessageID:    id,
		TemplateData: data,
	})
	// go-i18n also reports an error when it successfully fell back to the
	// bundle default language, so only an empty result means "unknown id".
	if err != nil && message == "" {
		return id
	}
	return message
}

// Normalize maps a language tag such as ca_ES.UTF-8, es-ES or EN_us to ca, es
// or en, and returns en for anything else (including empty, C and POSIX).
func Normalize(tag string) string {
	primary := strings.ToLower(strings.TrimSpace(tag))
	if index := strings.IndexAny(primary, "_.-@"); index >= 0 {
		primary = primary[:index]
	}
	for _, lang := range supportedLanguages {
		if primary == lang {
			return lang
		}
	}
	return defaultLanguage
}
