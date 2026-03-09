# Loop 0 Acceptance Criteria

## 1. Create writ via CLI
- [x] `sol writ create --title="Add tests for login" --world=myworld` prints an ID
- [x] ID format: sol-[0-9a-f]{8}

## 2. Dispatch to outpost
- [x] `sol cast <id> myworld` spawns a outpost in a fresh worktree
- [x] Worktree is at $SOL_HOME/myworld/outposts/{name}/world/
- [x] .claude/CLAUDE.md exists with writ details

## 3. GUPP — work context injected on start
- [x] Session starts with execution context visible
- [x] `sol prime` output includes writ title and instructions

## 4. sol resolve completes work
- [x] Branch pushed (or push attempted)
- [x] Tether file cleared
- [x] Writ status → resolve
- [x] Agent state → idle

## 5. Operator observability
- [x] `tmux attach -t sol-myworld-{name}` shows the agent working
- [x] `sqlite3 $SOL_HOME/.store/myworld.db "SELECT * FROM writs"` shows state

## 6. Crash recovery
- [x] Kill tmux session → tether file persists
- [x] Re-cast same item → agent picks up work

## 7. All tests pass
- [x] `make test` exits 0
- [x] Integration tests pass
- [x] CLI smoke tests pass
