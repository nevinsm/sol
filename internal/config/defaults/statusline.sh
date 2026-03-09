#!/usr/bin/env bash
# Sol statusline script for Claude Code.
# Reads JSON status from stdin (Claude Code status line protocol).
# When SOL_AGENT and SOL_WORLD are set, shows: [agent] world • model • ctx:N%
# Otherwise falls back to: cwd • model • ctx:N%

set -euo pipefail

# Read JSON from stdin.
input=$(cat)

# Extract fields from JSON using jq.
# Claude Code sends: {"cwd":"...","model":{"id":"...","display_name":"..."},"context_window":{"used_percentage":N,...}}
model=$(echo "$input" | jq -r '.model.id // empty')
context_pct=$(echo "$input" | jq -r '.context_window.used_percentage // empty')
cwd=$(echo "$input" | jq -r '.cwd // empty')

# Shorten model name (e.g., "claude-sonnet-4-20250514" → "Sonnet 4")
short_model="$model"
case "$model" in
  *opus*4*)   short_model="Opus 4" ;;
  *sonnet*4*) short_model="Sonnet 4" ;;
  *haiku*4*)  short_model="Haiku 4" ;;
  *opus*3*)   short_model="Opus 3" ;;
  *sonnet*3*) short_model="Sonnet 3" ;;
  *haiku*3*)  short_model="Haiku 3" ;;
esac

# Build context string.
ctx=""
if [ -n "$context_pct" ]; then
  ctx="ctx:${context_pct}%"
fi

# Format output based on whether sol env vars are present.
if [ -n "${SOL_AGENT:-}" ] && [ -n "${SOL_WORLD:-}" ]; then
  # Agent mode: [agent] world • model • ctx:N%
  parts="[${SOL_AGENT}] ${SOL_WORLD}"
  [ -n "$short_model" ] && parts="$parts • $short_model"
  [ -n "$ctx" ] && parts="$parts • $ctx"
  echo "$parts"
else
  # Operator mode: cwd • model • ctx:N%
  # Shorten cwd to basename if it's a path.
  short_cwd="${cwd##*/}"
  [ -z "$short_cwd" ] && short_cwd="$cwd"
  parts="$short_cwd"
  [ -n "$short_model" ] && parts="$parts • $short_model"
  [ -n "$ctx" ] && parts="$parts • $ctx"
  echo "$parts"
fi
