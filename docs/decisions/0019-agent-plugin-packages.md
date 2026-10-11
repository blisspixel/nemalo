# 0019: Claude Code and Agent Plugins packages for the read-only MCP server

Status: accepted and implemented, 2026-10-11. An increment of milestone 6, over
the read-only server of [decision 0014](0014-read-only-mcp-server.md).

## Context

Hosts load MCP servers and skills from plugin packages, and the formats differ.
Checked against primary documentation on 2026-10-11:

- [Claude Code plugins](https://code.claude.com/docs/en/plugins-reference) read
  `.claude-plugin/plugin.json`, a root `.mcp.json`, and `skills/<name>/SKILL.md`.
  A `userConfig` option of type `directory` is asked for at install and
  substituted into MCP arguments as `${user_config.KEY}`. A repository becomes a
  [marketplace](https://code.claude.com/docs/en/plugin-marketplaces) through
  `.claude-plugin/marketplace.json`.
- [Agent Plugins 1.0.0](https://agent-plugins.org/specification) reads a root
  `plugin.json` and `mcp.json`, each with a required `$schema`, and the same
  `skills/` layout. It has no user configuration, and clients must not expand
  placeholders other than `PLUGIN_ROOT` and `PLUGIN_DATA`. The published
  [plugin](https://agent-plugins.org/schemas/1.0.0/plugin.schema.json) and
  [MCP](https://agent-plugins.org/schemas/1.0.0/mcp.schema.json) schemas allow
  no additional properties.
- [Agent Skills](https://agentskills.io/specification) frontmatter is limited to
  `name`, `description`, `license`, `compatibility`, `metadata`, and
  `allowed-tools`.

## Decision

`plugins/nemalo/` carries both layouts side by side; their file names do not
collide. The repository root holds `.claude-plugin/marketplace.json`, so
`/plugin marketplace add blisspixel/nemalo` then `/plugin install nemalo@nemalo`
installs it in Claude Code.

- Both packages run the installed `nemalo` executable from PATH. They bundle no
  binaries, run no installer, and fetch nothing.
- Claude Code asks for one library folder at install (`userConfig.scope`) and
  passes it as `--scope`. The Agent Plugins package cannot ask, so it starts
  `nemalo mcp serve` and relies on Nemalo's configured library and review
  folders; without either, the server refuses to start and says why.
- Network catalog tools stay off in both. A user who wants them adds an MCP
  server with `--allow-network` themselves.
- One `SKILL.md`, limited to the portable fields, explains the tools, scopes,
  continuation cursors, evidence interpretation, and that changes go through the
  CLI the user runs. Instructions in it are guidance, not permission enforcement;
  the server's scope check and read-only tools are the enforcement.
- Package versions equal the executable version. A Go test checks both
  manifests, both MCP configurations, the marketplace entry, the skill
  frontmatter, and that every tool the skill names exists on the server.

## Consequences

Agent hosts get the same read-only tools as `nemalo mcp serve` without manual
MCP configuration. Each release updates the manifest versions with
`internal/cli.Version`. Write tools, durable jobs, and host compatibility on
macOS and Linux remain open.

## Validation

`claude plugin validate --strict` passes for the plugin and the marketplace with
Claude Code 2.1.296 on Windows. In a headless Claude Code session loading the
package with `--plugin-dir`, with the folder option supplied through settings
`pluginConfigs`, the skill loaded as `nemalo:nemalo`, the server started from
PATH, and a question about the library was answered through `library_list`,
reporting all seven tracks of a recording in declared order. The Agent Plugins
files were checked field by field against the published 1.0.0 schemas; no
Agent Plugins host was tested.
