package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type tutorTraceEvent struct {
	Name   string  `json:"name"`
	AtMS   float64 `json:"at_ms"`
	Detail string  `json:"detail,omitempty"`
}

// Cached input is a subset of InputTokens, not an additional chargeable total.
type tutorTokenUsage struct {
	InputTokens           int64  `json:"input_tokens"`
	CachedInputTokens     int64  `json:"cached_input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	ReasoningOutputTokens *int64 `json:"reasoning_output_tokens,omitempty"`
}

type tutorTraceRecord struct {
	ID                  string            `json:"id"`
	StartedAt           time.Time         `json:"started_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	Status              string            `json:"status"`
	Effort              string            `json:"effort"`
	AnalyticsDisabled   bool              `json:"analytics_disabled"`
	MinimalInstructions bool              `json:"minimal_instructions"`
	PromptBytes         int               `json:"prompt_bytes"`
	CorrectedBytes      int               `json:"corrected_bytes"`
	Changed             bool              `json:"changed"`
	Events              []tutorTraceEvent `json:"events"`
	Usage               *tutorTokenUsage  `json:"usage,omitempty"`
	Error               string            `json:"error,omitempty"`
}

type tutorTrace struct {
	mu     sync.Mutex
	start  time.Time
	home   string
	record tutorTraceRecord
}

func tutorMetricsDir(home string) string {
	return filepath.Join(home, "english-guard", "metrics")
}

func startTutorTrace(promptBytes int) *tutorTrace {
	home, err := codexHome()
	if err != nil {
		return nil
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil
	}
	now := time.Now()
	trace := &tutorTrace{home: home, start: now, record: tutorTraceRecord{
		ID:        fmt.Sprintf("%d-%s", now.UnixMilli(), hex.EncodeToString(nonce[:])),
		StartedAt: now.UTC(), UpdatedAt: now.UTC(), Status: "running", Effort: "low", AnalyticsDisabled: true, MinimalInstructions: true, PromptBytes: promptBytes,
	}}
	trace.mark("prepare_started", "")
	pruneTutorTraces(home)
	return trace
}

func (t *tutorTrace) persistLocked() {
	t.record.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(tutorMetricsDir(t.home), 0700); err != nil {
		return // Metrics must not break a grammar check.
	}
	file, err := os.CreateTemp(tutorMetricsDir(t.home), ".trace-*")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	if err := json.NewEncoder(file).Encode(t.record); err != nil {
		file.Close()
		return
	}
	if file.Close() == nil {
		_ = os.Rename(file.Name(), filepath.Join(tutorMetricsDir(t.home), t.record.ID+".json"))
	}
}

func (t *tutorTrace) eventLocked(name, detail string) {
	for _, event := range t.record.Events {
		if event.Name == name {
			return
		}
	}
	t.record.Events = append(t.record.Events, tutorTraceEvent{Name: name, Detail: detail, AtMS: float64(time.Since(t.start).Microseconds()) / 1000})
}

func (t *tutorTrace) mark(name, detail string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.eventLocked(name, detail)
	t.persistLocked()
}

func (t *tutorTrace) workerFinished(correctedBytes int, changed bool, err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.record.CorrectedBytes, t.record.Changed = correctedBytes, changed
	if err != nil {
		t.record.Status, t.record.Error = "failed", err.Error()
		t.eventLocked("failed", "")
	} else {
		t.record.Status = "ready"
		t.eventLocked("result_ready", "")
	}
	t.persistLocked()
}

func (t *tutorTrace) choiceStarted() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.record.Status = "choosing"
	t.eventLocked("choice.started", "")
	t.persistLocked()
}

func (t *tutorTrace) choiceFinished(accepted bool) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.record.Status = "cancelled"
	if accepted {
		t.record.Status = "completed"
	}
	t.eventLocked("choice.completed", "")
	t.persistLocked()
}

func (t *tutorTrace) cliEvent(line []byte) {
	if t == nil {
		return
	}
	var event struct {
		Type string `json:"type"`
		Item struct {
			Type string `json:"type"`
		} `json:"item"`
		Usage *tutorTokenUsage `json:"usage"`
	}
	if json.Unmarshal(line, &event) != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch event.Type {
	case "thread.started", "turn.started", "turn.failed", "turn.completed":
		t.eventLocked(event.Type, "")
		if event.Usage != nil {
			t.record.Usage = event.Usage
		}
	case "item.started", "item.completed":
		if event.Item.Type == "agent_message" {
			name := "model.response_started"
			if event.Type == "item.completed" {
				name = "model.response_completed"
			}
			t.eventLocked(name, "")
		} else if event.Item.Type == "reasoning" && event.Type == "item.started" {
			t.eventLocked("model.reasoning_started", "")
		} else {
			return
		}
	case "error":
		t.eventLocked("cli.error", "Codex reported an error")
	default:
		return
	}
	t.persistLocked()
}

func (t *tutorTrace) diagnostic(line []byte) {
	// Record categories only: raw CLI output can contain prompt text.
	lower := strings.ToLower(string(line))
	if strings.Contains(lower, "catalog") && (strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") || strings.Contains(lower, "failed")) {
		t.mark("catalog_warning", "Model catalog refresh failed or timed out")
	} else if strings.Contains(lower, "retry") || strings.Contains(lower, "reconnecting") || strings.Contains(lower, "stream disconnected") {
		t.mark("connection_retry", "Codex reported a connection retry")
	}
}

// exec.Cmd waits for its stdout/stderr writer goroutines before returning.
// Parsing incrementally preserves the observed arrival time of each CLI event.
type tutorEventWriter struct {
	mu     sync.Mutex
	trace  *tutorTrace
	stderr bool
	buffer []byte
}

func (w *tutorEventWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer = append(w.buffer, p...)
	for {
		end := bytes.IndexByte(w.buffer, '\n')
		if end < 0 {
			break
		}
		w.line(w.buffer[:end])
		w.buffer = w.buffer[end+1:]
	}
	if len(w.buffer) > 1024*1024 {
		w.buffer = nil
	}
	return len(p), nil
}

func (w *tutorEventWriter) line(line []byte) {
	if w.stderr {
		w.trace.diagnostic(line)
	} else {
		w.trace.cliEvent(line)
	}
}

func (w *tutorEventWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buffer) > 0 {
		w.line(w.buffer)
		w.buffer = nil
	}
}

func readTutorTraces(home string) ([]tutorTraceRecord, error) {
	entries, err := os.ReadDir(tutorMetricsDir(home))
	if os.IsNotExist(err) {
		return []tutorTraceRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]tutorTraceRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tutorMetricsDir(home), entry.Name()))
		if err != nil {
			continue
		}
		var record tutorTraceRecord
		if json.Unmarshal(data, &record) == nil && record.ID != "" {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].StartedAt.After(records[j].StartedAt) })
	if len(records) > 100 {
		records = records[:100]
	}
	return records, nil
}

func pruneTutorTraces(home string) {
	entries, err := os.ReadDir(tutorMetricsDir(home))
	if err != nil {
		return
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for i := 100; i < len(names); i++ {
		_ = os.Remove(filepath.Join(tutorMetricsDir(home), names[i]))
	}
}
