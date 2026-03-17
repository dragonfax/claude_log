# Claude Code Transcript Format

Notes on the JSONL transcript files written by Claude Code, for reference when adding features.

## Location

```
~/.claude/projects/<project-dir>/<session-uuid>.jsonl
~/.claude/projects/<project-dir>/<session-uuid>/subagents/agent-<type>-<hash>.jsonl
```

Each project directory name is the absolute path of the working directory with `/` replaced by `-`.

## File format

Each line is a JSON object (JSONL). Entry types:

| `type`                  | Description |
|-------------------------|-------------|
| `user`                  | User message or tool result |
| `assistant`             | Claude's response, may contain tool_use blocks |
| `progress`              | Hook progress events |
| `queue-operation`       | Message enqueue events |
| `file-history-snapshot` | File state snapshots for undo |
| `system`                | Hook summaries, errors (`subtype: "stop_hook_summary"`) |

## Common fields on all entries

```json
{
  "type": "user",
  "uuid": "...",
  "parentUuid": "...",
  "timestamp": "2026-03-15T18:43:56.329Z",
  "sessionId": "...",
  "isSidechain": false,
  "userType": "external",
  "cwd": "/path/to/project",
  "version": "2.1.71",
  "gitBranch": "main"
}
```

## Main context vs. subagents

- `"isSidechain": false` — main context (the session JSONL file)
- `"isSidechain": true` — subagent context (files in the `subagents/` subdirectory)

Subagent entries also have `"agentId": "<hash>"`. To process only what's in the main context, skip any entry where `isSidechain` is true.

## User messages

Initial user turn (no tool results):

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": "the user's message text"
  },
  "permissionMode": "default",
  "uuid": "...",
  "timestamp": "..."
}
```

`permissionMode` is present on user entries but does not reliably indicate whether a permission prompt was shown for any given tool call. Do not use it for permission detection.

## Tool result messages (user type)

When a tool call completes, a `user` entry is appended with the result:

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "tool_use_id": "toolu_01ABC...",
        "type": "tool_result",
        "content": "output text",
        "is_error": false
      }
    ]
  },
  "toolUseResult": {
    "stdout": "output text",
    "stderr": "",
    "interrupted": false,
    "isImage": false,
    "noOutputExpected": false
  },
  "sourceToolAssistantUUID": "uuid-of-assistant-message-that-called-the-tool"
}
```

`content` in a tool_result block can be either a plain string or an array of `{"type": "text", "text": "..."}` objects.

## Assistant messages with tool calls

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "model": "claude-sonnet-4-6",
    "id": "msg_...",
    "content": [
      {
        "type": "tool_use",
        "id": "toolu_01ABC...",
        "name": "Bash",
        "input": {
          "command": "ls -la"
        },
        "caller": {
          "type": "direct"
        }
      }
    ],
    "stop_reason": "tool_use",
    "usage": {
      "input_tokens": 100,
      "cache_creation_input_tokens": 205,
      "cache_read_input_tokens": 24663,
      "output_tokens": 136
    }
  },
  "uuid": "...",
  "requestId": "req_..."
}
```

`caller.type` is always `"direct"` in observed transcripts.

## Tool input fields by tool name

| Tool         | Key input fields |
|--------------|-----------------|
| `Bash`       | `command` |
| `Read`       | `file_path`, optional `offset`, `limit` |
| `Write`      | `file_path`, `content` |
| `Edit`       | `file_path`, `old_string`, `new_string` |
| `Grep`       | `pattern`, `path`, `glob`, `output_mode` |
| `Glob`       | `pattern`, `path` |
| `WebFetch`   | `url` |
| `WebSearch`  | `query` |
| `Agent`      | `description`, `subagent_type`, `prompt`, optional `run_in_background`, `isolation` |

## Agent (subagent) tool calls

Tool use input:
```json
{
  "type": "tool_use",
  "name": "Agent",
  "input": {
    "description": "Short description of task",
    "subagent_type": "Explore",
    "prompt": "Full prompt text..."
  }
}
```

`subagent_type` values observed: `"Explore"`, `"Plan"`, `"general-purpose"`, `"claude-code-guide"`, `"browser-proxy"`

The corresponding tool result in the main context:

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "tool_use_id": "toolu_...",
        "type": "tool_result",
        "content": [
          {"type": "text", "text": "<subagent response text>"},
          {"type": "text", "text": "agentId: abc123\n<usage>total_tokens: 143000\ntool_uses: 15\nduration_ms: 191670</usage>"}
        ]
      }
    ]
  },
  "toolUseResult": {
    "status": "completed",
    "agentId": "abc123",
    "content": [{"type": "text", "text": "<subagent response text>"}],
    "totalDurationMs": 191670,
    "totalTokens": 143000,
    "totalToolUseCount": 15,
    "usage": {
      "input_tokens": 0,
      "cache_creation_input_tokens": 1109,
      "cache_read_input_tokens": 138411,
      "output_tokens": 3480
    }
  }
}
```

`toolUseResult.totalTokens` is the total token count across the entire subagent run.

## Token usage fields

On assistant messages, `message.usage` contains:

```json
{
  "input_tokens": 1,
  "cache_creation_input_tokens": 205,
  "cache_read_input_tokens": 24663,
  "output_tokens": 136,
  "server_tool_use": {
    "web_search_requests": 0,
    "web_fetch_requests": 0
  }
}
```

## Hook-related entries

`type: "system"` with `subtype: "stop_hook_summary"` records hook execution results:

```json
{
  "type": "system",
  "subtype": "stop_hook_summary",
  "toolUseID": "toolu_...",
  "hookCount": 2,
  "hookErrors": ["[Verification Required] ..."],
  "preventedContinuation": false,
  "hasOutput": true,
  "level": "suggestion"
}
```

`toolUseID` links back to the tool_use entry that triggered the hook.
