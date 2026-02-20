.PHONY: build install clean test test-coverage e2e publish publish-tap release publish-homebrew publish-npm unpublish setup-hooks

BINARY := prysm

build:
	go build -o $(BINARY) ./cmd/prysm

install:
	go install ./cmd/prysm

clean:
	rm -f $(BINARY)

test:
	go test ./...

# Run tests with coverage for packages that support it (excludes cmd/plugins that may have tool requirements).
test-coverage:
	go test ./internal/api/... ./internal/config/... ./internal/derp/... ./internal/plugin/... ./internal/session/... ./internal/util/... -coverprofile=coverage.out
	go tool cover -func=coverage.out

e2e: build
	./scripts/e2e.sh

# Publish to cli repo (github.com/prysmsh/cli). Builds release artifacts,
# publishes to GitHub, and updates the Homebrew formula. Run: make publish VERSION=x.y.z
publish:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make publish VERSION=x.y.z"; exit 1; fi
	@./scripts/release_artifacts.sh $(VERSION)
	@if [ -z "$(SKIP_HOMEBREW)" ]; then $(MAKE) publish-homebrew VERSION=$(VERSION); fi
	@if [ -z "$(SKIP_NPM)" ]; then $(MAKE) publish-npm VERSION=$(VERSION); fi

# Commit and push the homebrew-tap (run after publish). Requires VERSION.
publish-tap:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make publish-tap VERSION=x.y.z"; exit 1; fi
	@cd .. && git -C homebrew-tap add Formula/cli.rb && \
		git -C homebrew-tap diff --cached --quiet && echo "No formula changes to commit" || \
		(git -C homebrew-tap commit -m "Update cli to v$(VERSION)" && git -C homebrew-tap push)

# Full release: publish + publish-tap. Run: make release VERSION=x.y.z
release: publish publish-tap

# Update only the Homebrew formula (artifacts must already exist in dist/releases/VERSION).
# Use when re-running formula update without re-publishing to GitHub.
publish-homebrew:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make publish-homebrew VERSION=x.y.z"; exit 1; fi
	@./scripts/update_homebrew_formula.sh $(VERSION)

# Publish to npm. Artifacts must already exist in dist/releases/VERSION.
# Run: make publish-npm VERSION=x.y.z
publish-npm:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make publish-npm VERSION=x.y.z"; exit 1; fi
	@./scripts/publish_npm.sh $(VERSION)

# Unpublish a version: delete GitHub release/tag and deprecate npm packages.
# Run: make unpublish VERSION=x.y.z
# Use: make unpublish VERSION=x.y.z DEPRECATE_ONLY=1 if npm publish was >72h ago.
unpublish:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make unpublish VERSION=x.y.z"; exit 1; fi
	@./scripts/unpublish.sh $(VERSION) $(if $(SKIP_GITHUB),--skip-github) $(if $(SKIP_NPM),--skip-npm) $(if $(DEPRECATE_ONLY),--deprecate-only)

# Install git pre-commit hook for secret scanning (requires gitleaks).
setup-hooks:
	cp scripts/pre-commit .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit
	@echo "Pre-commit hook installed."
