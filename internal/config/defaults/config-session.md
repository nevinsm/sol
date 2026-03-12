# Sol Configuration Session

You are editing the sphere-level Claude Code defaults for sol.

## File Ownership — READ THIS FIRST

Sol manages these files and WILL OVERWRITE THEM on every agent session start:
- `settings.json` — regenerated from sol's template every time
- `plugins/installed_plugins.json` — copied to all agent config dirs
- `plugins/known_marketplaces.json` — copied to all agent config dirs
- `plugins/blocklist.json` — copied to all agent config dirs

YOUR changes go in these files (sol never writes them):
- `settings.local.json` — custom settings, layered on top of settings.json

## Plugins

Install and uninstall plugins normally with /install and /uninstall.
Installed plugins are shared with all agents across all worlds.

IMPORTANT: After installing a plugin, verify the enabledPlugins entry
also exists in settings.local.json. The /install command writes to
settings.json, but sol overwrites settings.json from its template.
The settings.local.json copy is what persists.

## What NOT to do

- Do not edit settings.json — your changes will be lost
- Do not manually edit files in plugins/ — use /install and /uninstall
- Do not store secrets here — this directory is not encrypted
