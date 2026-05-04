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

# Shorten model name, preserving minor version when present:
#   claude-sonnet-4-20250514   → Sonnet 4
#   claude-sonnet-4-5-20251022 → Sonnet 4.5
#   claude-3-5-sonnet-20240620 → Sonnet 3.5
short_model="$model"
family=""
case "$model" in
  *opus*)   family="Opus" ;;
  *sonnet*) family="Sonnet" ;;
  *haiku*)  family="Haiku" ;;
esac
if [ -n "$family" ]; then
  fl=$(echo "$family" | tr '[:upper:]' '[:lower:]')
  major=""
  minor=""
  # Major/minor are 1–2 digits so the trailing 8-digit date stamp is not
  # mis-captured as a version component.
  # Claude 4-series ordering: ...-FAMILY-MAJOR(-MINOR)?-DATE
  if [[ "$model" =~ -${fl}-([0-9]{1,2})(-([0-9]{1,2}))?- ]]; then
    major="${BASH_REMATCH[1]}"
    minor="${BASH_REMATCH[3]:-}"
  # Claude 3-series ordering: ...-MAJOR(-MINOR)?-FAMILY-DATE
  elif [[ "$model" =~ -([0-9]{1,2})(-([0-9]{1,2}))?-${fl}- ]]; then
    major="${BASH_REMATCH[1]}"
    minor="${BASH_REMATCH[3]:-}"
  fi
  if [ -n "$major" ]; then
    if [ -n "$minor" ]; then
      short_model="${family} ${major}.${minor}"
    else
      short_model="${family} ${major}"
    fi
  fi
fi

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
