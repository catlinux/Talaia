module github.com/catlinux/Talaia/core

go 1.26.0

require (
	connectrpc.com/connect/v2 v2.0.1
	github.com/go-chi/chi/v5 v5.5.0
	github.com/jackc/pgx/v5 v5.11.0
	github.com/nicksnyder/go-i18n/v2 v2.6.1
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/pressly/goose/v3 v3.29.0
	golang.org/x/crypto v0.58.0
	golang.org/x/term v0.47.0
)

require (
	golang.org/x/sys v0.49.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

require (
	github.com/catlinux/Talaia/proto v0.0.0-00010101000000-000000000000
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0
)

replace github.com/catlinux/Talaia/proto => ../proto
