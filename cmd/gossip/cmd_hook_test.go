package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/loopstate"
	"github.com/yigitkonur/gossip/internal/statedir"
)

// ---------- readLastAssistantMessage fixtures ----------

func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}
}

func TestReadLastAssistantMessage_PicksFinalTurn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	writeJSONL(t, path,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"do it"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"thinking"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"continue"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"final answer [COMPLETION]"}]}}`,
	)
	text, pending, err := readLastAssistantMessage(path, 32*1024)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if pending {
		t.Errorf("pending tool_use should be false")
	}
	if !strings.Contains(text, "[COMPLETION]") {
		t.Errorf("text missing tag: %q", text)
	}
	if strings.Contains(text, "thinking") {
		t.Errorf("should return ONLY the last assistant turn, got %q", text)
	}
}

func TestReadLastAssistantMessage_DetectsPendingToolUse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	writeJSONL(t, path,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"running tool"},{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`,
	)
	text, pending, err := readLastAssistantMessage(path, 32*1024)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !pending {
		t.Errorf("pending tool_use should be true")
	}
	if text != "running tool" {
		t.Errorf("text = %q", text)
	}
}

func TestReadLastAssistantMessage_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	text, pending, err := readLastAssistantMessage(path, 32*1024)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if text != "" || pending {
		t.Errorf("expected empty+false, got %q %v", text, pending)
	}
}

func TestReadLastAssistantMessage_IgnoresNonAssistant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	writeJSONL(t, path,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"foo"}]}}`,
		`{"type":"summary","summary":"n/a"}`,
		`{"type":"system","content":"hook fired"}`,
	)
	text, _, err := readLastAssistantMessage(path, 32*1024)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if text != "" {
		t.Errorf("should ignore non-assistant lines, got %q", text)
	}
}

// ---------- containsAnyTag ----------

func TestContainsAnyTag_WordBoundary(t *testing.T) {
	cases := []struct {
		text string
		tags []string
		want bool
	}{
		{"task done. [COMPLETION]", []string{"COMPLETION"}, true},
		{"COMPLETION achieved", []string{"COMPLETION"}, true},
		{"completion reached", []string{"COMPLETION"}, true},
		{"RECOMPLETION ignored", []string{"COMPLETION"}, false},
		{"COMPLETIONS plural", []string{"COMPLETION"}, false},
		{"no tag at all", []string{"DONE", "COMPLETION"}, false},
		{"[DONE]", []string{"DONE", "COMPLETION"}, true},
		{"", []string{"DONE"}, false},
		{"anything", nil, false},
	}
	for _, tc := range cases {
		if got := containsAnyTag(tc.text, tc.tags); got != tc.want {
			t.Errorf("containsAnyTag(%q, %v) = %v, want %v", tc.text, tc.tags, got, tc.want)
		}
	}
}

// ---------- hook orchestration ----------

func withHookHooks(t *testing.T, cfg config.Config, bridge func(context.Context, string, int) (string, bool, string)) func() {
	t.Helper()
	sd := statedir.New(t.TempDir())
	_ = sd.Ensure()

	prevSender := hookBridgeSender
	prevCfg := hookLoadConfig
	prevState := hookStateDir

	hookBridgeSender = bridge
	hookLoadConfig = func() config.Config { return cfg }
	hookStateDir = func() *statedir.StateDir { return sd }

	return func() {
		hookBridgeSender = prevSender
		hookLoadConfig = prevCfg
		hookStateDir = prevState
	}
}

func defaultLoopCfg() config.Config {
	c := config.Config{}
	c.Loop = config.LoopConfig{
		Enabled:          true,
		MaxIterations:    3,
		PerTurnTimeoutMs: 5_000,
		CompletionTags:   []string{"COMPLETION"},
		ApprovalTags:     []string{"COMPLETED", "APPROVED"},
	}
	return c
}

func makeTranscript(t *testing.T, assistantText string, withToolUse bool) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	line := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": assistantText}},
		},
	}
	if withToolUse {
		line["message"].(map[string]any)["content"] = append(
			line["message"].(map[string]any)["content"].([]any),
			map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{}},
		)
	}
	b, _ := json.Marshal(line)
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return path
}

func runStopAndCapture(t *testing.T, in stopHookInput) (stopHookDecision, string) {
	t.Helper()
	body, _ := json.Marshal(in)
	var out bytes.Buffer
	if err := runHookStop(context.Background(), bytes.NewReader(body), &out); err != nil {
		t.Fatalf("runHookStop: %v", err)
	}
	if out.Len() == 0 {
		return stopHookDecision{}, ""
	}
	var dec stopHookDecision
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &dec); err != nil {
		t.Fatalf("decode output %q: %v", out.String(), err)
	}
	return dec, out.String()
}

func TestRunHookStop_NoCompletionTagIsSilent(t *testing.T) {
	cfg := defaultLoopCfg()
	defer withHookHooks(t, cfg, func(_ context.Context, _ string, _ int) (string, bool, string) {
		t.Fatalf("bridge should not be called when no tag")
		return "", false, ""
	})()

	path := makeTranscript(t, "just chatting, no tag", false)
	dec, out := runStopAndCapture(t, stopHookInput{SessionID: "s1", TranscriptPath: path})
	if out != "" || dec.Decision != "" {
		t.Errorf("expected silent exit, got output %q decision %q", out, dec.Decision)
	}
}

func TestRunHookStop_PendingToolUseIsSilent(t *testing.T) {
	cfg := defaultLoopCfg()
	defer withHookHooks(t, cfg, func(_ context.Context, _ string, _ int) (string, bool, string) {
		t.Fatalf("bridge should not be called mid-tool-use")
		return "", false, ""
	})()

	path := makeTranscript(t, "I'll check. [COMPLETION]", true)
	dec, out := runStopAndCapture(t, stopHookInput{SessionID: "s1", TranscriptPath: path})
	if out != "" || dec.Decision != "" {
		t.Errorf("expected silent exit on pending tool_use, got %q %+v", out, dec)
	}
}

func TestRunHookStop_LoopDisabledEnvIsSilent(t *testing.T) {
	cfg := defaultLoopCfg()
	defer withHookHooks(t, cfg, func(_ context.Context, _ string, _ int) (string, bool, string) {
		t.Fatalf("bridge should not be called when GOSSIP_LOOP_DISABLE=1")
		return "", false, ""
	})()
	t.Setenv("GOSSIP_LOOP_DISABLE", "1")
	path := makeTranscript(t, "done [COMPLETION]", false)

	dec, out := runStopAndCapture(t, stopHookInput{SessionID: "s1", TranscriptPath: path})
	if out != "" || dec.Decision != "" {
		t.Errorf("expected silent exit, got %+v / %q", dec, out)
	}
}

func TestRunHookStop_StopHookActiveIsSilent(t *testing.T) {
	cfg := defaultLoopCfg()
	defer withHookHooks(t, cfg, func(_ context.Context, _ string, _ int) (string, bool, string) {
		t.Fatalf("bridge should not be called when stop_hook_active=true")
		return "", false, ""
	})()
	path := makeTranscript(t, "Done. [COMPLETION]", false)
	dec, out := runStopAndCapture(t, stopHookInput{SessionID: "s1", TranscriptPath: path, StopHookActive: true})
	if out != "" || dec.Decision != "" {
		t.Errorf("expected silent exit on re-entry, got %+v / %q", dec, out)
	}
}

func TestRunHookStop_UsesConfiguredTagsInPrompts(t *testing.T) {
	cfg := defaultLoopCfg()
	cfg.Loop.CompletionTags = []string{"READY_FOR_REVIEW"}
	cfg.Loop.ApprovalTags = []string{"SHIP_IT"}

	var gotText string
	defer withHookHooks(t, cfg, func(_ context.Context, text string, _ int) (string, bool, string) {
		gotText = text
		return "Please add error handling.", true, ""
	})()

	path := makeTranscript(t, "Task finished. [READY_FOR_REVIEW]", false)
	dec, _ := runStopAndCapture(t, stopHookInput{SessionID: "s1", TranscriptPath: path})
	if !strings.Contains(gotText, "[SHIP_IT]") {
		t.Errorf("bridge prefix should use configured approval tag SHIP_IT: %q", gotText)
	}
	if strings.Contains(gotText, "[COMPLETED]") {
		t.Errorf("bridge prefix must not contain hardcoded [COMPLETED] when ApprovalTags is overridden: %q", gotText)
	}
	if !strings.Contains(dec.Reason, "[READY_FOR_REVIEW]") {
		t.Errorf("rejection continuation should reference configured completion tag: %q", dec.Reason)
	}
	if strings.Contains(dec.Reason, "[COMPLETION]") {
		t.Errorf("rejection continuation must not reference hardcoded [COMPLETION]: %q", dec.Reason)
	}
}

func TestRenderApprovalInstruction_ListsMultipleTags(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, "include [COMPLETED] in your reply"},
		{[]string{"COMPLETED"}, "include [COMPLETED] in your reply"},
		{[]string{"SHIP_IT"}, "include [SHIP_IT] in your reply"},
		{[]string{"COMPLETED", "LGTM"}, "include one of [COMPLETED] / [LGTM] in your reply"},
	}
	for _, tc := range cases {
		if got := renderApprovalInstruction(tc.in); got != tc.want {
			t.Errorf("renderApprovalInstruction(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRunHookStop_PushesAndInjectsApprovalReason(t *testing.T) {
	cfg := defaultLoopCfg()
	var gotText string
	var gotWait int
	defer withHookHooks(t, cfg, func(_ context.Context, text string, waitMs int) (string, bool, string) {
		gotText = text
		gotWait = waitMs
		return "Looks good. [COMPLETED]", true, ""
	})()

	path := makeTranscript(t, "Task finished. Ready for review. [COMPLETION]", false)
	dec, _ := runStopAndCapture(t, stopHookInput{SessionID: "s1", TranscriptPath: path})

	if !strings.Contains(gotText, "gossip:review-request") {
		t.Errorf("bridge payload missing review prefix: %q", gotText)
	}
	if !strings.Contains(gotText, "Task finished") {
		t.Errorf("bridge payload should include Claude summary: %q", gotText)
	}
	if gotWait != cfg.Loop.PerTurnTimeoutMs {
		t.Errorf("waitMs = %d, want %d", gotWait, cfg.Loop.PerTurnTimeoutMs)
	}
	if dec.Decision != "block" {
		t.Errorf("decision = %q, want block", dec.Decision)
	}
	if !strings.Contains(dec.Reason, "Codex approved") {
		t.Errorf("reason missing approval phrasing: %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "one-line confirmation") {
		t.Errorf("reason should instruct Claude to write confirmation: %q", dec.Reason)
	}
}

func TestRunHookStop_RejectionReasonIncludesFeedback(t *testing.T) {
	cfg := defaultLoopCfg()
	defer withHookHooks(t, cfg, func(_ context.Context, _ string, _ int) (string, bool, string) {
		return "Add error handling for nil input first.", true, ""
	})()

	path := makeTranscript(t, "Done. [COMPLETION]", false)
	dec, _ := runStopAndCapture(t, stopHookInput{SessionID: "s1", TranscriptPath: path})
	if dec.Decision != "block" {
		t.Fatalf("decision = %q, want block", dec.Decision)
	}
	if !strings.Contains(dec.Reason, "Codex review") {
		t.Errorf("reason missing review phrasing: %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "Add error handling") {
		t.Errorf("reason missing Codex feedback text: %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "[COMPLETION]") {
		t.Errorf("reason should instruct to re-emit [COMPLETION]: %q", dec.Reason)
	}
}

func TestRunHookStop_NoReplyReasonAsksUser(t *testing.T) {
	cfg := defaultLoopCfg()
	defer withHookHooks(t, cfg, func(_ context.Context, _ string, _ int) (string, bool, string) {
		return "", false, "timed out"
	})()

	path := makeTranscript(t, "Done. [COMPLETION]", false)
	dec, _ := runStopAndCapture(t, stopHookInput{SessionID: "s1", TranscriptPath: path})
	if dec.Decision != "block" {
		t.Fatalf("decision = %q, want block on silence", dec.Decision)
	}
	if !strings.Contains(dec.Reason, "timed out") {
		t.Errorf("reason should pass through bridge error: %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "retry or abandon") {
		t.Errorf("reason should offer retry/abandon choice: %q", dec.Reason)
	}
}

func TestRunHookStop_IterationCapEscalatesAndResets(t *testing.T) {
	cfg := defaultLoopCfg()
	cfg.Loop.MaxIterations = 2
	sd := statedir.New(t.TempDir())
	_ = sd.Ensure()

	prevSender := hookBridgeSender
	prevCfg := hookLoadConfig
	prevState := hookStateDir
	defer func() {
		hookBridgeSender = prevSender
		hookLoadConfig = prevCfg
		hookStateDir = prevState
	}()

	// Pre-seed loop state at the cap.
	_ = loopstate.WithLock(sd.LoopStateFile(), func(s *loopstate.State) error {
		*s = loopstate.Reset("s1", 2)
		s.Iteration = 2
		return nil
	})
	hookLoadConfig = func() config.Config { return cfg }
	hookStateDir = func() *statedir.StateDir { return sd }
	hookBridgeSender = func(_ context.Context, _ string, _ int) (string, bool, string) {
		t.Fatalf("bridge should not be called when at iteration cap")
		return "", false, ""
	}

	path := makeTranscript(t, "Still going. [COMPLETION]", false)
	dec, _ := runStopAndCapture(t, stopHookInput{SessionID: "s1", TranscriptPath: path})
	if dec.Decision != "block" {
		t.Fatalf("decision = %q, want block on cap", dec.Decision)
	}
	if !strings.Contains(dec.Reason, "cap") {
		t.Errorf("reason should mention cap: %q", dec.Reason)
	}

	got, err := loopstate.Load(sd.LoopStateFile())
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got.Iteration != 0 {
		t.Errorf("iteration not reset after cap: %d", got.Iteration)
	}
	if got.TerminatedReason != "cap" {
		t.Errorf("terminatedReason = %q, want cap", got.TerminatedReason)
	}
}

func TestRunHookStop_ResetsStateOnNewSession(t *testing.T) {
	cfg := defaultLoopCfg()
	sd := statedir.New(t.TempDir())
	_ = sd.Ensure()
	_ = loopstate.WithLock(sd.LoopStateFile(), func(s *loopstate.State) error {
		*s = loopstate.Reset("session-OLD", cfg.Loop.MaxIterations)
		s.Iteration = 2
		return nil
	})
	prevSender := hookBridgeSender
	prevCfg := hookLoadConfig
	prevState := hookStateDir
	defer func() {
		hookBridgeSender = prevSender
		hookLoadConfig = prevCfg
		hookStateDir = prevState
	}()
	hookLoadConfig = func() config.Config { return cfg }
	hookStateDir = func() *statedir.StateDir { return sd }
	hookBridgeSender = func(_ context.Context, _ string, _ int) (string, bool, string) {
		return "rejecting for now.", true, ""
	}

	path := makeTranscript(t, "fresh task [COMPLETION]", false)
	_, _ = runStopAndCapture(t, stopHookInput{SessionID: "session-NEW", TranscriptPath: path})

	got, _ := loopstate.Load(sd.LoopStateFile())
	if got.SessionID != "session-NEW" {
		t.Errorf("state session = %q, want session-NEW", got.SessionID)
	}
	if got.Iteration != 1 {
		t.Errorf("iteration = %d, want 1 (reset then incremented)", got.Iteration)
	}
}

// ---------- session-start ----------

func TestRunHookSessionStart_EmitsAdditionalContextAndResetsState(t *testing.T) {
	cfg := defaultLoopCfg()
	sd := statedir.New(t.TempDir())
	_ = sd.Ensure()
	_ = loopstate.WithLock(sd.LoopStateFile(), func(s *loopstate.State) error {
		*s = loopstate.Reset("OLD", cfg.Loop.MaxIterations)
		s.Iteration = 4
		return nil
	})

	prevCfg := hookLoadConfig
	prevState := hookStateDir
	defer func() {
		hookLoadConfig = prevCfg
		hookStateDir = prevState
	}()
	hookLoadConfig = func() config.Config { return cfg }
	hookStateDir = func() *statedir.StateDir { return sd }

	in := sessionStartHookInput{SessionID: "NEW"}
	body, _ := json.Marshal(in)
	var out bytes.Buffer
	if err := runHookSessionStart(context.Background(), bytes.NewReader(body), &out); err != nil {
		t.Fatalf("runHookSessionStart: %v", err)
	}

	var doc sessionStartOutput
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &doc); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, out.String())
	}
	if doc.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("hookEventName = %q", doc.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(doc.HookSpecificOutput.AdditionalContext, "[COMPLETION]") {
		t.Errorf("additionalContext should teach [COMPLETION]: %s", doc.HookSpecificOutput.AdditionalContext)
	}
	if !strings.Contains(doc.HookSpecificOutput.AdditionalContext, "consult_codex") {
		t.Errorf("additionalContext should mention consult_codex: %s", doc.HookSpecificOutput.AdditionalContext)
	}

	got, _ := loopstate.Load(sd.LoopStateFile())
	if got.SessionID != "NEW" {
		t.Errorf("state sessionID = %q, want NEW", got.SessionID)
	}
	if got.Iteration != 0 {
		t.Errorf("state iteration not reset: %d", got.Iteration)
	}
}

func TestRunHookSessionStart_EmitsNothingWhenLoopDisabled(t *testing.T) {
	cfg := defaultLoopCfg()
	cfg.Loop.Enabled = false
	sd := statedir.New(t.TempDir())

	prevCfg := hookLoadConfig
	prevState := hookStateDir
	defer func() {
		hookLoadConfig = prevCfg
		hookStateDir = prevState
	}()
	hookLoadConfig = func() config.Config { return cfg }
	hookStateDir = func() *statedir.StateDir { return sd }

	in := sessionStartHookInput{SessionID: "X"}
	body, _ := json.Marshal(in)
	var out bytes.Buffer
	if err := runHookSessionStart(context.Background(), bytes.NewReader(body), &out); err != nil {
		t.Fatalf("runHookSessionStart: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output when loop disabled, got %q", out.String())
	}
}
