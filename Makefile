.PHONY: build install clean test publish publish-tap release publish-homebrew

BINARY := prysm

build:
	go build -o $(BINARY) ./cmd/prysm

install:
	go install ./cmd/prysm

clean:
	rm -f $(BINARY)

test:
	go test ./...

# Publish to prysm-cli repo (github.com/prysmsh/cli). Builds release artifacts,
# publishes to GitHub, and updates the Homebrew formula. Run: make publish VERSION=x.y.z
publish:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make publish VERSION=x.y.z"; exit 1; fi
	@./scripts/release_artifacts.sh $(VERSION)
	@if [ -z "$(SKIP_HOMEBREW)" ]; then $(MAKE) publish-homebrew VERSION=$(VERSION); fi

# Commit and push the homebrew-tap (run after publish). Requires VERSION.
publish-tap:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make publish-tap VERSION=x.y.z"; exit 1; fi
	@cd .. && git -C homebrew-tap add Formula/prysm-cli.rb && \
		git -C homebrew-tap diff --cached --quiet && echo "No formula changes to commit" || \
		(git -C homebrew-tap commit -m "Update prysm-cli to v$(VERSION)" && git -C homebrew-tap push)

# Full release: publish + publish-tap. Run: make release VERSION=x.y.z
release: publish publish-tap

# Update only the Homebrew formula (artifacts must already exist in dist/releases/VERSION).
# Use when re-running formula update without re-publishing to GitHub.
publish-homebrew:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make publish-homebrew VERSION=x.y.z"; exit 1; fi
	@./scripts/update_homebrew_formula.sh $(VERSION)
