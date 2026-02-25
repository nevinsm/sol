.PHONY: build test test-e2e install clean

GT_TEST_HOME  := /tmp/gt-test
GT_TEST_RIG   := myrig
GT_TEST_AGENT := Toast

build:
	go build -o bin/gt .

test:
	go test ./...

# Full end-to-end test: create agent, create work item, sling, verify, done, verify, clean up.
# Cleans up all artifacts: GT_HOME dir, git worktrees, polecat branches, tmux sessions.
test-e2e: build
	@export GT_HOME=$(GT_TEST_HOME) RIG=$(GT_TEST_RIG) AGENT=$(GT_TEST_AGENT) && \
	cleanup() { \
		echo "=== E2E: cleanup ==="; \
		sleep 2; \
		tmux kill-session -t gt-$$RIG-$$AGENT 2>/dev/null || true; \
		rm -rf $(GT_TEST_HOME); \
		git worktree prune; \
		git for-each-ref --format='%(refname:short)' 'refs/heads/polecat/$(GT_TEST_AGENT)/' | xargs -r git branch -D 2>/dev/null || true; \
		git for-each-ref --format='%(refname:short)' 'refs/remotes/origin/polecat/$(GT_TEST_AGENT)/' | sed 's|origin/||' | xargs -r -I{} git push origin --delete {} 2>/dev/null || true; \
	} && \
	trap cleanup EXIT && \
	\
	echo "=== E2E: setup ===" && \
	rm -rf $(GT_TEST_HOME) && \
	tmux kill-session -t gt-$$RIG-$$AGENT 2>/dev/null || true && \
	git worktree prune && \
	\
	echo "=== E2E: create agent ===" && \
	bin/gt agent create $$AGENT --rig=$$RIG && \
	bin/gt agent list --rig=$$RIG && \
	\
	echo "=== E2E: create work item ===" && \
	ITEM=$$(bin/gt store create --db=$$RIG --title="E2E test item" --description="Automated end-to-end test") && \
	echo "Created: $$ITEM" && \
	\
	echo "=== E2E: sling ===" && \
	bin/gt sling $$ITEM $$RIG --agent=$$AGENT && \
	\
	echo "=== E2E: verify sling ===" && \
	bin/gt session list && \
	bin/gt store get $$ITEM --db=$$RIG && \
	bin/gt prime --rig=$$RIG --agent=$$AGENT && \
	test -f $(GT_TEST_HOME)/$$RIG/polecats/$$AGENT/.hook && \
	\
	echo "=== E2E: done ===" && \
	GT_RIG=$$RIG GT_AGENT=$$AGENT bin/gt done && \
	\
	echo "=== E2E: verify done ===" && \
	bin/gt store get $$ITEM --db=$$RIG && \
	bin/gt agent list --rig=$$RIG && \
	test ! -f $(GT_TEST_HOME)/$$RIG/polecats/$$AGENT/.hook && \
	\
	echo "=== E2E: PASSED ==="

install: build
	cp bin/gt /usr/local/bin/gt

clean:
	rm -rf bin/
