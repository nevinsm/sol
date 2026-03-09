#!/usr/bin/env bash
# Sol API key helper for Claude Code.
# Returns the current OAuth access token from broker-managed credentials.
# The broker refreshes .credentials.json every 5 minutes; this script
# is called periodically by Claude Code (controlled by
# CLAUDE_CODE_API_KEY_HELPER_TTL_MS) to pick up rotated tokens.

set -euo pipefail

jq -r '.claudeAiOauth.accessToken' "$CLAUDE_CONFIG_DIR/.credentials.json"
