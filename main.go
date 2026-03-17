package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const maxSummary = 100

func transcriptDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error finding home directory: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(home, ".claude", "projects")
}

// --- JSON types ---

type Entry struct {
	Type       string          `json:"type"`
	UUID       string          `json:"uuid"`
	ParentUUID string          `json:"parentUuid"`
	Timestamp  time.Time       `json:"timestamp"`
	IsSidechain bool           `json:"isSidechain"`
	Message    *Message        `json:"message"`
	ToolUseResult *ToolUseResult `json:"toolUseResult"`
}

type Message struct {
	Role    string        `json:"role"`
	Content MessageContent `json:"content"`
}

// MessageContent can be a string or []ContentBlock
type MessageContent struct {
	Blocks []ContentBlock
	Text   string
}

func (m *MessageContent) UnmarshalJSON(data []byte) error {
	// Try array first
	var blocks []ContentBlock
	if err := json.Unmarshal(data, &blocks); err == nil {
		m.Blocks = blocks
		return nil
	}
	// Try string
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.Text = s
		return nil
	}
	return nil
}

type ContentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Text      string          `json:"text"`
	ToolUseID string          `json:"tool_use_id"`
	Content   ToolResultContent `json:"content"`
	IsError   bool            `json:"is_error"`
}

// ToolResultContent can be string or []ContentBlock
type ToolResultContent struct {
	Text   string
	Blocks []ContentBlock
}

func (t *ToolResultContent) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		t.Text = s
		return nil
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(data, &blocks); err == nil {
		t.Blocks = blocks
		return nil
	}
	return nil
}

func (t *ToolResultContent) Size() int {
	if t.Text != "" {
		return len(t.Text)
	}
	total := 0
	for _, b := range t.Blocks {
		total += len(b.Text)
	}
	return total
}

type ToolUseResult struct {
	Stdout        string         `json:"stdout"`
	Stderr        string         `json:"stderr"`
	Status        string         `json:"status"`
	AgentID       string         `json:"agentId"`
	TotalTokens   int            `json:"totalTokens"`
	TotalDurationMs int          `json:"totalDurationMs"`
	TotalToolUseCount int        `json:"totalToolUseCount"`
	Content       []ContentBlock `json:"content"`
}

// --- Session file discovery ---

type SessionFile struct {
	Path    string
	ModTime time.Time
}

func findSessionFiles() ([]SessionFile, error) {
	dir := transcriptDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []SessionFile
	for _, projectEntry := range entries {
		if !projectEntry.IsDir() {
			continue
		}
		projectDir := filepath.Join(dir, projectEntry.Name())
		projectEntries, err := os.ReadDir(projectDir)
		if err != nil {
			continue
		}
		for _, f := range projectEntries {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			files = append(files, SessionFile{
				Path:    filepath.Join(projectDir, f.Name()),
				ModTime: info.ModTime(),
			})
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})
	return files, nil
}

// --- Parsing ---

type ToolCall struct {
	ToolName     string
	InputSummary string
	OutputBytes  int
	Timestamp    time.Time
	IsAgent      bool
	AgentTokens  int
}

type SessionReport struct {
	Path       string
	ModTime    time.Time
	ProjectDir string
	ToolCalls  []ToolCall
}

func summarizeInput(name string, raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return truncate(string(raw), maxSummary)
	}

	switch name {
	case "LSP":
		op, _ := m["operation"].(string)
		fp, _ := m["filePath"].(string)
		if op != "" && fp != "" {
			return truncate(fmt.Sprintf("%s %s", op, fp), maxSummary)
		}
		if op != "" {
			return op
		}
	case "Bash":
		if cmd, ok := m["command"].(string); ok {
			cmd = strings.ReplaceAll(cmd, "\n", " ")
			cmd = strings.ReplaceAll(cmd, "\t", " ")
			return cmd
		}
	case "Read":
		if p, ok := m["file_path"].(string); ok {
			return p
		}
	case "Write", "Edit":
		if p, ok := m["file_path"].(string); ok {
			return p
		}
	case "Grep":
		pattern, _ := m["pattern"].(string)
		path, _ := m["path"].(string)
		if path != "" {
			return truncate(fmt.Sprintf("%s in %s", pattern, path), maxSummary)
		}
		return truncate(pattern, maxSummary)
	case "Glob":
		if p, ok := m["pattern"].(string); ok {
			return p
		}
	case "WebFetch":
		if u, ok := m["url"].(string); ok {
			return truncate(u, maxSummary)
		}
	case "WebSearch":
		if q, ok := m["query"].(string); ok {
			return truncate(q, maxSummary)
		}
	case "Agent":
		if d, ok := m["description"].(string); ok {
			return truncate(d, maxSummary)
		}
	}

	// Fallback: compact JSON
	b, _ := json.Marshal(m)
	return truncate(string(b), maxSummary)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func parseSession(path string) (*SessionReport, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	report := &SessionReport{
		Path:    path,
		ModTime: info.ModTime(),
	}

	// Maps to correlate tool calls with their results
	type pendingCall struct {
		name    string
		summary string
		ts      time.Time
		isAgent bool
	}
	pending := map[string]pendingCall{} // tool_use_id -> call info
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		// Skip sidechain (subagent) entries — we only care about main context
		if entry.IsSidechain {
			continue
		}

		switch entry.Type {
		case "user":
			if entry.Message == nil {
				continue
			}

			// Process tool results
			for _, block := range entry.Message.Content.Blocks {
				if block.Type != "tool_result" {
					continue
				}
				call, ok := pending[block.ToolUseID]
				if !ok {
					continue
				}
				delete(pending, block.ToolUseID)

				tc := ToolCall{
					ToolName:     call.name,
					InputSummary: call.summary,
					Timestamp:    call.ts,
					IsAgent:      call.isAgent,
				}

				if call.isAgent && entry.ToolUseResult != nil {
					tc.AgentTokens = entry.ToolUseResult.TotalTokens
					// OutputBytes not set for agents; IsAgent flag handles display
				} else {
					tc.OutputBytes = block.Content.Size()
				}

				report.ToolCalls = append(report.ToolCalls, tc)
			}

		case "assistant":
			if entry.Message == nil {
				continue
			}
			for _, block := range entry.Message.Content.Blocks {
				if block.Type != "tool_use" {
					continue
				}
				pending[block.ID] = pendingCall{
					name:    block.Name,
					summary: summarizeInput(block.Name, block.Input),
					ts:      entry.Timestamp,
					isAgent: block.Name == "Agent",
				}
			}
		}
	}

	return report, scanner.Err()
}

// --- Display ---

func printReport(r *SessionReport, sessionNum int) (bool, error) {
	lspCalls := []ToolCall{}
	bigOutputCalls := []ToolCall{}

	for _, tc := range r.ToolCalls {
		if tc.ToolName == "LSP" {
			lspCalls = append(lspCalls, tc)
		}
		if tc.IsAgent || tc.OutputBytes > 1000 {
			bigOutputCalls = append(bigOutputCalls, tc)
		}
	}

	if len(lspCalls) == 0 && len(bigOutputCalls) == 0 {
		return false, nil
	}

	// Project name from path
	parts := strings.Split(r.Path, string(os.PathSeparator))
	var project, session string
	if len(parts) >= 2 {
		project = parts[len(parts)-2]
		session = strings.TrimSuffix(parts[len(parts)-1], ".jsonl")
		if len(project) > 50 {
			project = "..." + project[len(project)-47:]
		}
		if len(session) > 8 {
			session = session[:8]
		}
	}

	w := os.Stdout
	if _, err := fmt.Fprintf(w, "\n=== Session %d: %s / %s (%s) ===\n",
		sessionNum, project, session, r.ModTime.Format("2006-01-02 15:04")); err != nil {
		return false, err
	}

	if len(lspCalls) > 0 {
		if _, err := fmt.Fprintln(w, "\n  LSP calls:"); err != nil {
			return false, err
		}
		for _, tc := range lspCalls {
			if _, err := fmt.Fprintf(w, "    %s\n", tc.InputSummary); err != nil {
				return false, err
			}
		}
	}

	if len(bigOutputCalls) > 0 {
		if _, err := fmt.Fprintln(w, "\n  Large tool outputs / subagents:"); err != nil {
			return false, err
		}
		for _, tc := range bigOutputCalls {
			var err error
			if tc.IsAgent {
				_, err = fmt.Fprintf(w, "    Agent(%d tokens): %s\n", tc.AgentTokens, tc.InputSummary)
			} else {
				_, err = fmt.Fprintf(w, "    %s(%s): %s\n", tc.ToolName, formatBytes(tc.OutputBytes), tc.InputSummary)
			}
			if err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

func formatBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/1024/1024)
}

func main() {
	signal.Ignore(syscall.SIGPIPE)

	files, err := findSessionFiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading transcripts: %v\n", err)
		os.Exit(1)
	}

	if len(files) == 0 {
		fmt.Println("No session transcripts found.")
		return
	}

	displayNum := 0
	for _, sf := range files {
		report, err := parseSession(sf.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not parse %s: %v\n", sf.Path, err)
			continue
		}
		displayNum++
		printed, err := printReport(report, displayNum)
		if !printed {
			displayNum--
		}
		if err != nil {
			if errors.Is(err, io.ErrClosedPipe) || strings.Contains(err.Error(), "broken pipe") {
				return
			}
			fmt.Fprintf(os.Stderr, "write error: %v\n", err)
			return
		}
	}
}
