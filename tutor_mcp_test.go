package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestMCPDedicatedTutorChoice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := enableTutorSession(home, "chat-a"); err != nil {
		t.Fatal(err)
	}
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	server := &tutorMCPServer{
		in: inputReader, out: outputWriter,
		grammar: func(prompt string) (tutorResult, error) {
			if prompt != "I has a bug." {
				t.Errorf("worker got wrong prompt: %q", prompt)
			}
			trace := startTutorTrace(len(prompt))
			trace.workerFinished(len("I have a bug."), true, nil)
			return tutorResult{CorrectedPrompt: "I have a bug.", trace: trace}, nil
		},
		pending: make(map[int64]chan mcpMessage),
	}
	done := make(chan error, 1)
	go func() { done <- server.serve() }()
	defer func() {
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
		<-done
	}()
	reader := bufio.NewReader(outputReader)
	write := func(message any) {
		t.Helper()
		if err := json.NewEncoder(inputWriter).Encode(message); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string]json.RawMessage {
		t.Helper()
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var response map[string]json.RawMessage
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	write(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	if result := read()["result"]; !strings.Contains(string(result), "protocolVersion") {
		t.Fatalf("missing initialize result: %s", result)
	}
	write(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	if result := read()["result"]; !strings.Contains(string(result), "check_prompt") || strings.Contains(string(result), "review_correction") {
		t.Fatalf("wrong tool schema: %s", result)
	}
	write(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{
		"name": "check_prompt", "arguments": map[string]string{"session_id": "chat-a", "prompt": "I has a bug."},
	}})
	request := read()
	if string(request["method"]) != `"elicitation/create"` || !strings.Contains(string(request["params"]), "I have a bug.") {
		t.Fatalf("missing full correction in native choice: %v", request)
	}
	write(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{
		"action": "accept", "content": map[string]string{"choice": "Use correction"},
	}})
	response := read()
	var toolResult struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(response["result"], &toolResult); err != nil || len(toolResult.Content) != 1 {
		t.Fatalf("bad tool result: %v, %v", response, err)
	}
	if !strings.Contains(toolResult.Content[0].Text, `I have a bug.`) || !strings.Contains(toolResult.Content[0].Text, `selected_prompt_json`) {
		t.Fatalf("missing selected correction: %s", toolResult.Content[0].Text)
	}
	runs, err := readTutorTraces(home)
	if err != nil || len(runs) != 1 || runs[0].Status != "completed" {
		t.Fatalf("native-choice metrics missing: %+v %v", runs, err)
	}
	names := map[string]bool{}
	for _, event := range runs[0].Events {
		names[event.Name] = true
	}
	if !names["result_ready"] || !names["choice.started"] || !names["choice.completed"] {
		t.Fatalf("native choice was not timed separately: %+v", runs[0].Events)
	}
}

func TestMCPDedicatedTutorGuards(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	server := &tutorMCPServer{grammar: func(string) (tutorResult, error) {
		t.Fatal("grammar ran for inactive chat")
		return tutorResult{}, nil
	}}
	if got := server.checkPrompt("chat-a", "I has a bug."); got != nil {
		t.Fatalf("inactive chat invoked Tutor: %v", got)
	}
	if got := server.checkPrompt("chat-a", "Привет"); got == nil || got["decision"] != "block" {
		t.Fatalf("letter rule did not run: %v", got)
	}
	if err := enableTutorSession(home, "chat-a"); err != nil {
		t.Fatal(err)
	}
	if got := server.checkPrompt("chat-a", "Tutor off"); got != nil {
		t.Fatalf("control prompt invoked Tutor: %v", got)
	}
	server.grammar = func(prompt string) (tutorResult, error) { return tutorResult{CorrectedPrompt: prompt}, nil }
	if got := server.checkPrompt("chat-a", "I have a bug."); got != nil {
		t.Fatalf("unchanged GPT result should proceed without a choice: %v", got)
	}
	server.grammar = func(string) (tutorResult, error) { return tutorResult{}, errors.New("worker unavailable") }
	if got := server.checkPrompt("chat-a", "I has a bug."); got == nil || got["decision"] != "block" {
		t.Fatalf("worker error did not return block: %v", got)
	}
}

func TestMCPChoiceActions(t *testing.T) {
	for _, tc := range []struct {
		name, action, choice, wantChoice string
		wantErr                          bool
	}{
		{name: "keep", action: "accept", choice: "Keep original", wantChoice: "Keep original"},
		{name: "cancel", action: "cancel", wantErr: true},
		{name: "decline", action: "decline", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			server := &tutorMCPServer{out: writer, pending: make(map[int64]chan mcpMessage)}
			type result struct {
				choice string
				err    error
			}
			finished := make(chan result, 1)
			go func() {
				choice, err := server.askChoice("Suggested prompt:\nI have a bug.")
				finished <- result{choice: choice, err: err}
			}()
			var request struct {
				ID int64 `json:"id"`
			}
			if err := json.NewDecoder(reader).Decode(&request); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(map[string]any{"action": tc.action, "content": map[string]string{"choice": tc.choice}})
			if err != nil {
				t.Fatal(err)
			}
			server.mu.Lock()
			server.pending[request.ID] <- mcpMessage{Result: encoded}
			server.mu.Unlock()
			got := <-finished
			if got.choice != tc.wantChoice || (got.err != nil) != tc.wantErr {
				t.Fatalf("choice=%q err=%v", got.choice, got.err)
			}
		})
	}
}
