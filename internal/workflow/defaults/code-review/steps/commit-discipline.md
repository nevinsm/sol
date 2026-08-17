# Commit Discipline Review

Review the commit history on the branch for good practices.

Good commits make the codebase easier to understand, bisect, and revert.

Examine the git log for the branch. Read commit messages and diffs.

**Look for:**
- Giant "WIP" or "fix" commits
  - Multiple unrelated changes in one commit
  - Commits that touch 20+ files across different features
  - Entire feature in a single monolithic commit

- Poor commit messages
  - "stuff", "update", "asdf", "fix", "wip"
  - No context about WHY the change was made
  - Messages that describe the what but not the why

- Unatomic commits
  - Feature + refactor + bugfix in the same commit
  - Should be separable logical units
  - Test changes mixed with implementation in ways that obscure the diff

- Commit ordering issues
  - Refactoring after the feature instead of before
  - Test fixes for bugs introduced in earlier commits on the same branch

**Questions to answer:**
- Could this history be bisected effectively to find a regression?
- Would a reviewer understand the progression of changes?
- Are commits atomic — one logical change each?
- Is the branch telling a coherent story?

Note: this branch's history is squashed to a single commit by forge on merge
(message: `{writ title} (writ-id)`), so branch commit messages don't need to
follow any particular format — they only need to be clear enough for review
and for the agent's own crash recovery.
