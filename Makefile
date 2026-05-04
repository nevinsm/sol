.PHONY: build test test-short test-integration test-flaky test-e2e install clean release-snapshot docs-validate docs-validate-cli api-schemas api-docs api

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

SOL_TEST_HOME  := /tmp/sol-test
SOL_TEST_WORLD := myworld
SOL_TEST_AGENT := Toast

build:
	go build -ldflags "-X github.com/nevinsm/sol/cmd.version=$(VERSION)" -o bin/sol .

# `make test` runs the cli.md + ADR-status drift gate (cheap, deterministic) but
# does NOT run the full `docs-validate` because the broader documentation drift
# checks intentionally fail until the doc-reconciliation writ lands. The full
# checker is a separate target so CI can run it independently.
test: docs-validate-cli
	go test -race ./...

# Cheap drift gate: regenerated docs/cli.md must match the checked-in copy, and
# every ADR under docs/decisions/ must declare Status: on line 3. This subset
# is wired into `make test` so refactors that forget `sol docs generate` fail
# fast without dragging in the broader content drift checks.
docs-validate-cli: build
	./bin/sol docs generate --check
	@echo "=== ADR status lint ==="
	@fail=0; for f in docs/decisions/[0-9]*.md; do \
		line3=$$(sed -n '3p' "$$f"); \
		case "$$line3" in \
			Status:*|status:*) ;; \
			*) echo "  MISSING Status: on line 3 — $$f"; fail=1 ;; \
		esac; \
	done; \
	if [ $$fail -ne 0 ]; then echo "ADR status lint failed"; exit 1; fi; \
	echo "  ok"

# Full doc drift gate: cli.md + ADR cross-references, workflow step counts,
# recovery matrix coverage, heartbeat paths, persona archetypes, and
# acceptance-doc test references. See internal/docvalidate/README.md.
#
# `sol docs validate` already runs the cli.md generation check; this target
# also runs the ADR status lint that has historically guarded ADR file shape.
#
# Intended for CI and the doc-reconciliation workflow. Not wired into
# `make test` because it currently fails with known drift items that a
# downstream writ will reconcile.
docs-validate: build
	./bin/sol docs validate
	@echo "=== ADR status lint ==="
	@fail=0; for f in docs/decisions/[0-9]*.md; do \
		line3=$$(sed -n '3p' "$$f"); \
		case "$$line3" in \
			Status:*|status:*) ;; \
			*) echo "  MISSING Status: on line 3 — $$f"; fail=1 ;; \
		esac; \
	done; \
	if [ $$fail -ne 0 ]; then echo "ADR status lint failed"; exit 1; fi; \
	echo "  ok"

test-short:
	go test -short -race ./...

test-integration:
	go test -race -count=1 ./test/integration/

# Known-flaky integration tests, quarantined out of `make test`.
# Each test is gated by SOL_RUN_FLAKY_TESTS in its own t.Skip guard.
test-flaky:
	SOL_RUN_FLAKY_TESTS=1 go test -race -run "TestDAGWorkflowE2E|TestMassDeathDegradation" ./test/integration/

# Full end-to-end test: create agent, create writ, cast, verify, resolve, verify, clean up.
# Cleans up all artifacts: SOL_HOME dir, git worktrees, outpost branches, tmux sessions.
test-e2e: build
	@export SOL_HOME=$(SOL_TEST_HOME) WORLD=$(SOL_TEST_WORLD) AGENT=$(SOL_TEST_AGENT) && \
	cleanup() { \
		echo "=== E2E: cleanup ==="; \
		sleep 2; \
		tmux kill-session -t sol-$$WORLD-$$AGENT 2>/dev/null || true; \
		rm -rf $(SOL_TEST_HOME); \
		git worktree prune; \
		git for-each-ref --format='%(refname:short)' 'refs/heads/outpost/$(SOL_TEST_AGENT)/' | xargs -r git branch -D 2>/dev/null || true; \
		git for-each-ref --format='%(refname:short)' 'refs/remotes/origin/outpost/$(SOL_TEST_AGENT)/' | sed 's|origin/||' | xargs -r -I{} git push origin --delete {} 2>/dev/null || true; \
	} && \
	trap cleanup EXIT && \
	\
	echo "=== E2E: setup ===" && \
	rm -rf $(SOL_TEST_HOME) && \
	tmux kill-session -t sol-$$WORLD-$$AGENT 2>/dev/null || true && \
	git worktree prune && \
	\
	echo "=== E2E: init world ===" && \
	bin/sol world init $$WORLD --source-repo=$$(pwd) && \
	\
	echo "=== E2E: create agent ===" && \
	bin/sol agent create $$AGENT --world=$$WORLD && \
	bin/sol agent list --world=$$WORLD && \
	\
	echo "=== E2E: create writ ===" && \
	ITEM=$$(bin/sol writ create --world=$$WORLD --title="E2E test item" --description="Automated end-to-end test") && \
	echo "Created: $$ITEM" && \
	\
	echo "=== E2E: cast ===" && \
	bin/sol cast $$ITEM --world=$$WORLD --agent=$$AGENT && \
	\
	echo "=== E2E: verify cast ===" && \
	bin/sol session list && \
	bin/sol writ status $$ITEM --world=$$WORLD && \
	bin/sol prime --world=$$WORLD --agent=$$AGENT && \
	test -d $(SOL_TEST_HOME)/$$WORLD/outposts/$$AGENT/.tether && \
	\
	echo "=== E2E: resolve ===" && \
	SOL_WORLD=$$WORLD SOL_AGENT=$$AGENT bin/sol resolve && \
	\
	echo "=== E2E: verify resolve ===" && \
	bin/sol writ status $$ITEM --world=$$WORLD && \
	bin/sol agent list --world=$$WORLD && \
	test ! -d $(SOL_TEST_HOME)/$$WORLD/outposts/$$AGENT/.tether && \
	\
	echo "=== E2E: PASSED ==="

install:
	go build -ldflags "-X github.com/nevinsm/sol/cmd.version=$(VERSION)" -o ~/.local/bin/sol .

release-snapshot:
	goreleaser release --snapshot --clean

api-schemas:
	go run ./cmd/sol-api-gen

api-docs:
	go run ./cmd/sol-api-doc

api: api-schemas api-docs

clean:
	rm -rf bin/
