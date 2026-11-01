package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestTutorTelemetryUsesCLIEventsWithoutPromptText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	trace := startTutorTrace(42)
	trace.mark("process_launch", "")
	trace.mark("process_started", "")
	stdout := &tutorEventWriter{trace: trace}
	stderr := &tutorEventWriter{trace: trace, stderr: true}
	const canary = "PRIVATE_PROMPT_CANARY"
	events := `{"type":"thread.started","thread_id":"private-thread"}` + "\n" +
		`{"type":"turn.started"}` + "\n" +
		`{"type":"item.completed","item":{"type":"agent_message","text":"` + canary + `"}}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":75,"output_tokens":10,"reasoning_output_tokens":4}}`
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		_, _ = stdout.Write([]byte(events[:20]))
		_, _ = stdout.Write([]byte(events[20:]))
		stdout.flush()
	}()
	go func() {
		defer group.Done()
		_, _ = stderr.Write([]byte("model catalog refresh timed out " + canary + "\n"))
		stderr.flush()
	}()
	group.Wait()
	trace.mark("process.exited", "")
	trace.workerFinished(40, true, nil)
	trace.choiceStarted()
	trace.choiceFinished(true)
	runs, err := readTutorTraces(home)
	if err != nil || len(runs) != 1 {
		t.Fatalf("metrics missing: %+v %v", runs, err)
	}
	run := runs[0]
	if run.Status != "completed" || run.Effort != "low" || run.Usage == nil || run.Usage.InputTokens != 100 || run.Usage.CachedInputTokens != 75 || run.Usage.OutputTokens != 10 {
		t.Fatalf("wrong run metrics: %+v", run)
	}
	observed := make(map[string]float64)
	for _, event := range run.Events {
		observed[event.Name] = event.AtMS
	}
	for _, event := range []string{"turn.started", "model.response_completed", "turn.completed", "catalog_warning", "result_ready", "choice.started", "choice.completed"} {
		if _, ok := observed[event]; !ok {
			t.Fatalf("missing observed event: %s", event)
		}
	}
	if observed["choice.started"] < observed["result_ready"] || observed["choice.completed"] < observed["choice.started"] {
		t.Fatal("choice wait was mixed with GPT processing")
	}
	path := filepath.Join(tutorMetricsDir(home), run.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), canary) || strings.Contains(string(data), "private-thread") {
		t.Fatal("metrics stored prompt, output, or thread content")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows uses inherited ACLs rather than POSIX permission bits.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("metrics are not private: %v %v", info, err)
	}
}

func TestTutorTelemetryFailureAndMissingUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	trace := startTutorTrace(5)
	trace.workerFinished(0, false, errors.New("GPT timed out"))
	runs, err := readTutorTraces(home)
	if err != nil || len(runs) != 1 || runs[0].Status != "failed" || runs[0].Usage != nil {
		t.Fatalf("failed run should not invent token usage: %+v %v", runs, err)
	}
	encoded, _ := json.Marshal(runs[0])
	if strings.Contains(string(encoded), "input_tokens") {
		t.Fatal("missing usage was serialized as a measured zero")
	}
}
