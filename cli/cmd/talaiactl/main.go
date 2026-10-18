// Command talaiactl is the Talaia command-line client.
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/catlinux/Talaia/cli/i18n"
)

// version is the version placeholder, overridden at build time.
var version = "dev"

func main() {
	lang := flag.String("lang", "", "output language (ca, es, en)")
	flag.Parse()

	language := i18n.Resolve(*lang, os.Getenv)
	localizer, err := i18n.New(language)
	if err != nil {
		fmt.Fprintln(os.Stderr, "talaiactl:", err)
		os.Exit(1)
	}

	if *lang != "" && !knownLanguage(*lang) {
		fmt.Fprintln(os.Stderr, localizer.T("error.unknownLanguage", map[string]any{"Language": *lang}))
	}

	fmt.Println(localizer.T("version.line", map[string]any{"Version": version}))
}

// knownLanguage reports whether the tag names one of the supported languages.
func knownLanguage(tag string) bool {
	primary := strings.ToLower(strings.TrimSpace(tag))
	if index := strings.IndexAny(primary, "_.-@"); index >= 0 {
		primary = primary[:index]
	}
	return slices.Contains(i18n.Supported(), primary)
}
