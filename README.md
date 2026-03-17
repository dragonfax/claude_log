# claude-log

Reads Claude Code session transcripts and reports what tool calls were made and what's bloating the main context window.

## What it shows

For each session (newest first), three sections:

- **LSP calls** — every LSP operation Claude made (file navigation, symbol lookup, etc.)
- **Large tool outputs / subagents** — tool results over 1KB injected into the main context, with their size; subagent (Agent tool) calls show total token count and description
- **Permission-required calls** — tool calls that triggered a user permission prompt (requires hook setup, see below)

Tool calls requiring user permission are marked with `[PERM]` in any section.

Only main-context activity is reported. Tool calls made inside subagents are excluded.

## Install

```
go install .
```

This builds and places the binary in `$GOBIN` (default: `~/go/bin`). Make sure that directory is on your `PATH`.

Or build manually and place wherever you like:

```
go build -o claude-log .
mv claude-log /usr/local/bin/
```

## Usage

```
claude-log watch        # show session reports
claude-log --help       # show all commands
```

## Permission tracking

To see which tool calls required your approval, register the PermissionRequest hook:

```
claude-log install-hook
```

This adds an entry to `~/.claude/settings.json` that fires `claude-log record-permission` whenever Claude Code shows a permission prompt. Permission events are stored in `~/.claude/claude_log/permissions.jsonl` and correlated with transcript entries when you run `claude-log watch`.

To remove the hook:

```
claude-log uninstall-hook
```

## Commands

```
claude-log watch              Show session reports with tool call details
claude-log install-hook       Register the PermissionRequest hook
claude-log uninstall-hook     Remove the PermissionRequest hook
claude-log record-permission  (internal) Called by the hook
```

## Requirements

Go 1.21+. No external dependencies.

Reads transcripts from `~/.claude/projects/` and permission events from `~/.claude/claude_log/permissions.jsonl`.
