# claude-log

Reads Claude Code session transcripts and reports what commands were run and what's bloating the main context window.

## What it shows

For each session (newest first):

- **Bash commands** — every shell command Claude ran, flagged with `[PERMISSION REQUIRED]` when the session was in a non-default permission mode (e.g. the user was prompted to approve actions)
- **Large tool outputs / subagents** — tool results over 1KB that were injected into the main context, with their size; subagent (Agent tool) calls show total token count and the description passed to the agent

Only main-context activity is reported. Tool calls made inside subagents, or subagents spawned by subagents, are excluded.

## Usage

```
go run main.go | less
go run main.go | head -100
```

Streams all sessions indefinitely. Pipe to `less`, `head`, etc. to control output. Exits cleanly when the pipe closes.

## Install

Build a binary and put it on your PATH:

```
go build -o claude-log .
mv claude-log /usr/local/bin/
```

Then just run:

```
claude-log | less
```

## Requirements

Go 1.21+. No external dependencies.

Reads transcripts from `~/.claude/projects/` (hardcoded).
