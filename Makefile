# Talaia monorepo build targets.
# The Go toolchain and the linters run on Linux; development happens on Windows.

GO_MODULES := core agent cli

.PHONY: dev build test lint i18n-check license-check

# Start the local development environment: database, core, agent and web.
dev:
	@bash scripts/dev.sh

# Build every Go module and write the binaries to bin/.
build:
	@mkdir -p bin
	@for module in $(GO_MODULES); do \
		echo "build: $$module"; \
		(cd $$module && go build -o ../bin/ ./...); \
	done

# Run the Go tests of every module.
test:
	@for module in $(GO_MODULES); do \
		echo "test: $$module"; \
		(cd $$module && go test ./...); \
	done

# Lint every Go module.
lint:
	@for module in $(GO_MODULES); do \
		echo "lint: $$module"; \
		(cd $$module && golangci-lint run); \
	done

# Check that every i18n key is present in ca, es and en.
i18n-check:
	@node scripts/i18n-check.mjs

# Check that every source file has its license header (replaced in a later phase).
license-check:
	@echo "license-check: not implemented yet"
