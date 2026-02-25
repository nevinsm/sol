# Loop 0 Acceptance Criteria

## 1. Create work item via store
- [x] `gt store create --title="Add tests for login" --db=myrig` prints an ID
- [x] ID format: gt-[0-9a-f]{8}

## 2. Dispatch to polecat
- [x] `gt sling <id> myrig` spawns a polecat in a fresh worktree
- [x] Worktree is at $GT_HOME/myrig/polecats/{name}/rig/
- [x] .claude/CLAUDE.md exists with work item details

## 3. GUPP — work context injected on start
- [x] Session starts with execution context visible
- [x] `gt prime` output includes work item title and instructions

## 4. gt done completes work
- [x] Branch pushed (or push attempted)
- [x] Hook file cleared
- [x] Work item status → done
- [x] Agent state → idle

## 5. Operator observability
- [x] `tmux attach -t gt-myrig-{name}` shows the agent working
- [x] `sqlite3 $GT_HOME/.store/myrig.db "SELECT * FROM work_items"` shows state

## 6. Crash recovery
- [x] Kill tmux session → hook file persists
- [x] Re-sling same item → agent picks up work

## 7. All tests pass
- [x] `make test` exits 0
- [x] Integration tests pass
- [x] CLI smoke tests pass
