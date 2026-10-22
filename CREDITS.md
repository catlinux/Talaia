# Credits

**Talaia** is developed and maintained by **CatLinux**. Copyright (C) 2026 CatLinux.

The original material of Talaia (the Go sources of `core`, `agent`, `cli` and `proto`, the web panel,
the scripts, the packaging, the documentation, and the logo and icons in `web/src/assets` and
`web/public`) is licensed under [WNCL-TP-1.0](LICENSE). The third-party components below are **not**
covered by that license: each one keeps its own license and notices.

The full license text and copyright notice of every component is in
[THIRD-PARTY-NOTICES.txt](THIRD-PARTY-NOTICES.txt), which also ships in the agent package
(`/usr/share/doc/talaia-agent/`) and in the core image (`/usr/share/doc/talaia/`). Regenerate it with
`node scripts/third-party-notices.mjs` after changing dependencies.

## Fonts

| Font | Author | License | Link |
| --- | --- | --- | --- |
| Familjen Grotesk | The Familjen Grotesk Project Authors | OFL-1.1 | https://github.com/Familjen-Sthlm/Familjen-Grotesk |
| IBM Plex Sans | IBM Corp. | OFL-1.1 | https://github.com/IBM/plex |
| JetBrains Mono | The JetBrains Mono Project Authors | OFL-1.1 | https://github.com/JetBrains/JetBrainsMono |

The fonts are bundled into the web panel through the `@fontsource` packages listed below.

## Go modules linked into the binaries

| Module | Version | License | Link |
| --- | --- | --- | --- |
| connectrpc.com/connect/v2 | v2.0.1 | Apache-2.0 | https://pkg.go.dev/connectrpc.com/connect/v2@v2.0.1 |
| github.com/cenkalti/backoff/v5 | v5.0.3 | MIT | https://pkg.go.dev/github.com/cenkalti/backoff/v5@v5.0.3 |
| github.com/coder/websocket | v1.8.15 | ISC | https://pkg.go.dev/github.com/coder/websocket@v1.8.15 |
| github.com/fxamacker/cbor/v2 | v2.9.4 | MIT | https://pkg.go.dev/github.com/fxamacker/cbor/v2@v2.9.4 |
| github.com/go-acme/lego/v4 | v4.35.2 | MIT | https://pkg.go.dev/github.com/go-acme/lego/v4@v4.35.2 |
| github.com/go-chi/chi/v5 | v5.5.0 | MIT | https://pkg.go.dev/github.com/go-chi/chi/v5@v5.5.0 |
| github.com/go-jose/go-jose/v4 | v4.1.4 | Apache-2.0 | https://pkg.go.dev/github.com/go-jose/go-jose/v4@v4.1.4 |
| github.com/go-viper/mapstructure/v2 | v2.5.0 | MIT | https://pkg.go.dev/github.com/go-viper/mapstructure/v2@v2.5.0 |
| github.com/go-webauthn/webauthn | v0.17.3 | BSD-3-Clause | https://pkg.go.dev/github.com/go-webauthn/webauthn@v0.17.3 |
| github.com/go-webauthn/x | v0.3.1 | BSD-3-Clause | https://pkg.go.dev/github.com/go-webauthn/x@v0.3.1 |
| github.com/golang-jwt/jwt/v5 | v5.3.1 | MIT | https://pkg.go.dev/github.com/golang-jwt/jwt/v5@v5.3.1 |
| github.com/google/go-tpm | v0.9.8 | Apache-2.0 | https://pkg.go.dev/github.com/google/go-tpm@v0.9.8 |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause | https://pkg.go.dev/github.com/google/uuid@v1.6.0 |
| github.com/jackc/pgpassfile | v1.0.0 | MIT | https://pkg.go.dev/github.com/jackc/pgpassfile@v1.0.0 |
| github.com/jackc/pgservicefile | v0.0.0-20240606120523-5a60cdf6a761 | MIT | https://pkg.go.dev/github.com/jackc/pgservicefile@v0.0.0-20240606120523-5a60cdf6a761 |
| github.com/jackc/pgx/v5 | v5.11.0 | MIT | https://pkg.go.dev/github.com/jackc/pgx/v5@v5.11.0 |
| github.com/jackc/puddle/v2 | v2.2.3 | MIT | https://pkg.go.dev/github.com/jackc/puddle/v2@v2.2.3 |
| github.com/mfridman/interpolate | v0.0.2 | MIT | https://pkg.go.dev/github.com/mfridman/interpolate@v0.0.2 |
| github.com/miekg/dns | v1.1.72 | BSD-3-Clause | https://pkg.go.dev/github.com/miekg/dns@v1.1.72 |
| github.com/nicksnyder/go-i18n/v2 | v2.6.1 | MIT | https://pkg.go.dev/github.com/nicksnyder/go-i18n/v2@v2.6.1 |
| github.com/ovh/go-ovh | v1.9.0 | BSD-3-Clause | https://pkg.go.dev/github.com/ovh/go-ovh@v1.9.0 |
| github.com/pelletier/go-toml/v2 | v2.4.3 | MIT | https://pkg.go.dev/github.com/pelletier/go-toml/v2@v2.4.3 |
| github.com/philhofer/fwd | v1.2.0 | MIT | https://pkg.go.dev/github.com/philhofer/fwd@v1.2.0 |
| github.com/pressly/goose/v3 | v3.29.0 | MIT | https://pkg.go.dev/github.com/pressly/goose/v3@v3.29.0 |
| github.com/sethvargo/go-retry | v0.5.0 | Apache-2.0 | https://pkg.go.dev/github.com/sethvargo/go-retry@v0.5.0 |
| github.com/shirou/gopsutil/v4 | v4.26.9 | BSD-3-Clause | https://pkg.go.dev/github.com/shirou/gopsutil/v4@v4.26.9 |
| github.com/tinylib/msgp | v1.6.4 | MIT | https://pkg.go.dev/github.com/tinylib/msgp@v1.6.4 |
| github.com/tklauser/go-sysconf | v0.4.0 | BSD-3-Clause | https://pkg.go.dev/github.com/tklauser/go-sysconf@v0.4.0 |
| github.com/tklauser/numcpus | v0.12.0 | Apache-2.0 | https://pkg.go.dev/github.com/tklauser/numcpus@v0.12.0 |
| github.com/x448/float16 | v0.8.4 | MIT | https://pkg.go.dev/github.com/x448/float16@v0.8.4 |
| go.uber.org/multierr | v1.11.0 | MIT | https://pkg.go.dev/go.uber.org/multierr@v1.11.0 |
| golang.org/x/crypto | v0.58.0 | BSD-3-Clause | https://pkg.go.dev/golang.org/x/crypto@v0.58.0 |
| golang.org/x/net | v0.61.0 | BSD-3-Clause | https://pkg.go.dev/golang.org/x/net@v0.61.0 |
| golang.org/x/oauth2 | v0.36.0 | BSD-3-Clause | https://pkg.go.dev/golang.org/x/oauth2@v0.36.0 |
| golang.org/x/sync | v0.24.0 | BSD-3-Clause | https://pkg.go.dev/golang.org/x/sync@v0.24.0 |
| golang.org/x/sys | v0.49.0 | BSD-3-Clause | https://pkg.go.dev/golang.org/x/sys@v0.49.0 |
| golang.org/x/term | v0.47.0 | BSD-3-Clause | https://pkg.go.dev/golang.org/x/term@v0.47.0 |
| golang.org/x/text | v0.43.0 | BSD-3-Clause | https://pkg.go.dev/golang.org/x/text@v0.43.0 |
| google.golang.org/protobuf | v1.36.12 | BSD-3-Clause | https://pkg.go.dev/google.golang.org/protobuf@v1.36.12 |
| gopkg.in/ini.v1 | v1.67.1 | Apache-2.0 | https://pkg.go.dev/gopkg.in/ini.v1@v1.67.1 |

## Packages bundled into the web panel

| Package | Version | License | Link |
| --- | --- | --- | --- |
| @babel/runtime | 7.29.10 | MIT | https://babel.dev/docs/en/next/babel-runtime |
| @fontsource/familjen-grotesk | 5.3.0 | OFL-1.1 | https://fontsource.org/fonts/familjen-grotesk |
| @fontsource/ibm-plex-sans | 5.3.0 | OFL-1.1 | https://fontsource.org/fonts/ibm-plex-sans |
| @fontsource/jetbrains-mono | 5.3.0 | OFL-1.1 | https://fontsource.org/fonts/jetbrains-mono |
| @xterm/addon-fit | 0.11.0 | MIT | https://github.com/xtermjs/xterm.js/tree/master#readme |
| @xterm/xterm | 6.0.0 | MIT | https://github.com/xtermjs/xterm.js#readme |
| ansi-regex | 5.0.1 | MIT | https://github.com/chalk/ansi-regex#readme |
| ansi-styles | 4.3.0 | MIT | https://github.com/chalk/ansi-styles#readme |
| camelcase | 5.3.1 | MIT | https://github.com/sindresorhus/camelcase#readme |
| cliui | 6.0.0 | ISC | https://github.com/yargs/cliui#readme |
| color-convert | 2.0.1 | MIT | https://github.com/Qix-/color-convert#readme |
| color-name | 1.1.4 | MIT | https://github.com/colorjs/color-name |
| decamelize | 1.2.0 | MIT | https://github.com/sindresorhus/decamelize#readme |
| dijkstrajs | 1.0.3 | MIT | https://github.com/tcort/dijkstrajs |
| emoji-regex | 8.0.0 | MIT | https://mths.be/emoji-regex |
| find-up | 4.1.0 | MIT | https://github.com/sindresorhus/find-up#readme |
| get-caller-file | 2.0.5 | ISC | https://github.com/stefanpenner/get-caller-file#readme |
| html-parse-stringify | 4.0.1 | MIT | https://github.com/i18next/html-parse-stringify |
| i18next | 26.5.2 | MIT | https://www.i18next.com |
| i18next-browser-languagedetector | 8.2.1 | MIT | https://github.com/i18next/i18next-browser-languageDetector |
| is-fullwidth-code-point | 3.0.0 | MIT | https://github.com/sindresorhus/is-fullwidth-code-point#readme |
| locate-path | 5.0.0 | MIT | https://github.com/sindresorhus/locate-path#readme |
| p-limit | 2.3.0 | MIT | https://github.com/sindresorhus/p-limit#readme |
| p-locate | 4.1.0 | MIT | https://github.com/sindresorhus/p-locate#readme |
| p-try | 2.2.0 | MIT | https://github.com/sindresorhus/p-try#readme |
| path-exists | 4.0.0 | MIT | https://github.com/sindresorhus/path-exists#readme |
| pngjs | 5.0.0 | MIT | https://github.com/lukeapage/pngjs |
| qrcode | 1.5.4 | MIT | http://github.com/soldair/node-qrcode |
| react | 19.3.1 | MIT | https://react.dev/ |
| react-dom | 19.3.1 | MIT | https://react.dev/ |
| react-i18next | 17.1.0 | MIT | https://github.com/i18next/react-i18next |
| require-directory | 2.1.1 | MIT | https://github.com/troygoode/node-require-directory/ |
| require-main-filename | 2.0.0 | ISC | https://github.com/yargs/require-main-filename#readme |
| scheduler | 0.28.0 | MIT | https://react.dev/ |
| set-blocking | 2.0.0 | ISC | https://github.com/yargs/set-blocking#readme |
| string-width | 4.2.3 | MIT | https://github.com/sindresorhus/string-width#readme |
| strip-ansi | 6.0.1 | MIT | https://github.com/chalk/strip-ansi#readme |
| use-sync-external-store | 1.7.0 | MIT | https://github.com/react/react#readme |
| which-module | 2.0.1 | ISC | https://github.com/nexdrew/which-module#readme |
| wrap-ansi | 6.2.0 | MIT | https://github.com/chalk/wrap-ansi#readme |
| y18n | 4.0.3 | ISC | https://github.com/yargs/y18n |
| yargs | 15.4.1 | MIT | https://yargs.js.org/ |
| yargs-parser | 18.1.3 | ISC | https://github.com/yargs/yargs-parser#readme |
