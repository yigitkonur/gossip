package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/loopstate"
	"github.com/yigitkonur/gossip/internal/statedir"
)

// stopHookInput is the JSON Claude Code writes to stdin for a Stop hook.
type stopHookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// sessionStartHookInput is the SessionStart hook stdin JSON.
type sessionStartHookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Source         string `json:"source"`
	Model          string `json:"model"`
}

// stopHookDecision is the JSON emitted on stdout (exit 0) to force Claude to
// continue or escalate via the `reason` field.
type stopHookDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type sessionStartOutput struct {
	HookSpecificOutput sessionStartSpecificOutput `json:"hookSpecificOutput"`
}

type sessionStartSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// hookBridgeSender is the seam used to push to Codex from a hook. Tests
// replace it with a canned-response stub so no daemon is needed.
var hookBridgeSender = func(ctx context.Context, text string, waitMs int) (replyText string, received bool, errMsg string) {
	r, _ := runBridgeSend(ctx, bridgeSendParams{Text: text, RequireReply: true, WaitMs: waitMs})
	return r.Text, r.Received, r.Error
}

// hookLoadConfig loads the project config via the existing config.Service
// (cwd-based). Tests override to supply a synthetic LoopConfig.
var hookLoadConfig = func() config.Config {
	return config.NewService("").LoadOrDefault()
}

// hookStateDir resolves the shared state dir. Tests point it at a tempdir.
var hookStateDir = func() *statedir.StateDir { return statedir.New("") }

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Claude Code hook handlers (invoked by .claude/settings.json)",
		Hidden: true,
	}
	cmd.AddCommand(newHookSessionStartCmd(), newHookStopCmd(), newHookUserPromptCmd())
	return cmd
}

func newHookSessionStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session-start",
		Short: "SessionStart handler: injects loop convention and resets stale state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookSessionStart(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

func newHookStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop handler: detects [COMPLETION], drives the review loop with Codex",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookStop(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

func newHookUserPromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "user-prompt",
		Short: "UserPromptSubmit handler: reserved stub, exits 0",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Reserved for future Codex-inbox surfacing. No output for v1.
			return nil
		},
	}
}

// runHookSessionStart emits the loop convention as additionalContext and
// resets loop-state.json when session_id has changed.
func runHookSessionStart(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	in, _ := parseSessionStartInput(stdin)
	cfg := hookLoadConfig()

	if cfg.Loop.Enabled && in.SessionID != "" {
		sd := hookStateDir()
		_ = sd.Ensure()
		_ = loopstate.WithLock(sd.LoopStateFile(), func(s *loopstate.State) error {
			if s.SessionID != in.SessionID {
				*s = loopstate.Reset(in.SessionID, cfg.Loop.MaxIterations)
			}
			return nil
		})
	}

	ctxText := renderSessionStartContext(cfg.Loop)
	if ctxText == "" {
		return nil
	}
	out := sessionStartOutput{
		HookSpecificOutput: sessionStartSpecificOutput{
			HookEventName:     "SessionStart",
			AdditionalContext: ctxText,
		},
	}
	return writeJSON(stdout, out)
}

// runHookStop implements the completion-loop decision table. See
// /Users/yigitkonur/.claude/plans/completion-etiketi-varsa-mesela-linear-hamster.md
// §4 for the prose version; this function is the faithful translation.
func runHookStop(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	in, err := parseStopInput(stdin)
	if err != nil {
		return nil // fail-open: stay invisible to Claude Code
	}
	if os.Getenv("GOSSIP_LOOP_DISABLE") != "" {
		return nil
	}
	cfg := hookLoadConfig()
	if !cfg.Loop.Enabled {
		return nil
	}
	if in.TranscriptPath == "" {
		return nil
	}

	tail, hasPendingToolUse, err := readLastAssistantMessage(in.TranscriptPath, 128*1024)
	if err != nil {
		return nil // best effort; never wedge Claude on a read error
	}
	if hasPendingToolUse {
		return nil // still working — never interfere mid tool-loop
	}
	if !containsAnyTag(tail, cfg.Loop.CompletionTags) {
		return nil
	}

	sd := hookStateDir()
	_ = sd.Ensure()
	statePath := sd.LoopStateFile()

	var decision stopHookDecision
	err = loopstate.WithLock(statePath, func(s *loopstate.State) error {
		if s.SessionID != in.SessionID {
			*s = loopstate.Reset(in.SessionID, cfg.Loop.MaxIterations)
		}
		if s.MaxIterations == 0 {
			s.MaxIterations = cfg.Loop.MaxIterations
		}
		if s.Iteration >= s.MaxIterations {
			decision = stopHookDecision{
				Decision: "block",
				Reason: fmt.Sprintf(
					"Completion-loop cap (%d iterations) reached without Codex approval. "+
						"Summarize the current state for the user, describe what Codex has been pushing back on, "+
						"and hand control back for a human decision. Do not re-emit [COMPLETION].",
					s.MaxIterations,
				),
			}
			s.TerminatedReason = "cap"
			s.Iteration = 0
			return nil
		}

		prefix := renderReviewPrefix(s.Iteration+1, s.MaxIterations)
		replyText, received, errMsg := hookBridgeSender(ctx, prefix+tail, cfg.Loop.PerTurnTimeoutMs)
		s.Iteration++

		if !received {
			reason := "Codex did not reply"
			if errMsg != "" {
				reason = fmt.Sprintf("Codex did not reply: %s", errMsg)
			}
			decision = stopHookDecision{
				Decision: "block",
				Reason: fmt.Sprintf(
					"%s. Summarize current status for the user and ask whether to retry or abandon the loop. "+
						"Do not re-emit [COMPLETION] unless the user asks you to.",
					reason,
				),
			}
			s.TerminatedReason = "codex-silent"
			return nil
		}

		if containsAnyTag(replyText, cfg.Loop.ApprovalTags) {
			decision = stopHookDecision{
				Decision: "block",
				Reason: fmt.Sprintf(
					"✅ Codex approved the work:\n\n%s\n\n"+
						"Write a one-line confirmation to the user that the task is complete. "+
						"Do not revise the work further and do not re-emit [COMPLETION].",
					replyText,
				),
			}
			s.LastCodexReplyID = "approved"
			s.TerminatedReason = "approved"
			s.Iteration = 0
			return nil
		}

		decision = stopHookDecision{
			Decision: "block",
			Reason: fmt.Sprintf(
				"Codex review (iteration %d/%d):\n\n%s\n\n"+
					"Revise your work based on this feedback. End your next response with [COMPLETION] "+
					"if you believe the revision addresses Codex's points.",
				s.Iteration, s.MaxIterations, replyText,
			),
		}
		s.LastCodexReplyID = "rejected"
		return nil
	})
	if err != nil {
		return nil
	}
	if decision.Decision == "" {
		return nil
	}
	return writeJSON(stdout, decision)
}

func parseStopInput(r io.Reader) (stopHookInput, error) {
	var in stopHookInput
	dec := json.NewDecoder(r)
	if err := dec.Decode(&in); err != nil && err != io.EOF {
		return stopHookInput{}, err
	}
	return in, nil
}

func parseSessionStartInput(r io.Reader) (sessionStartHookInput, error) {
	var in sessionStartHookInput
	dec := json.NewDecoder(r)
	if err := dec.Decode(&in); err != nil && err != io.EOF {
		return sessionStartHookInput{}, err
	}
	return in, nil
}

func writeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

func renderSessionStartContext(loop config.LoopConfig) string {
	if !loop.Enabled {
		return ""
	}
	completionTag := "COMPLETION"
	if len(loop.CompletionTags) > 0 {
		completionTag = loop.CompletionTags[0]
	}
	approvalTag := "COMPLETED"
	if len(loop.ApprovalTags) > 0 {
		approvalTag = loop.ApprovalTags[0]
	}
	return fmt.Sprintf(
		`## Gossip completion-loop protocol
You are collaborating with Codex through a gossip bridge. When you believe a task is complete, end your assistant message with the literal tag [%s] on its own line. A reviewer (Codex) will evaluate and respond:
  - Approval is signaled by Codex including [%s] (or any configured approval tag) in its reply. You will receive a continuation asking you to write a one-line confirmation — do not revise further.
  - Otherwise, Codex's feedback arrives as a new user turn; revise your work and re-emit [%s] when you believe the revision addresses the feedback.
Maximum loop iterations per task: %d. After that the user takes over.
Use the consult_codex MCP tool for targeted mid-turn consultations only; do not also emit [%s] in the same turn.`,
		completionTag, approvalTag, completionTag, loop.MaxIterations, completionTag,
	)
}

func renderReviewPrefix(iter, max int) string {
	return fmt.Sprintf(
		"[gossip:review-request iter=%d/max=%d]\n"+
			"Claude believes the task below is complete. Please review and:\n"+
			"  - If correct and complete, include [COMPLETED] in your reply.\n"+
			"  - Otherwise, describe specifically what needs to change.\n"+
			"Task summary from Claude:\n---\n",
		iter, max,
	)
}

// readLastAssistantMessage scans a Claude Code session JSONL file and
// returns the concatenated text of the final assistant turn, plus a flag
// indicating whether that turn has any pending tool_use blocks. Only the
// last maxBytes of the file are read; for sessions bigger than that, the
// message is assumed to span the tail and still resolves correctly in
// typical usage (one turn rarely exceeds 128 KiB of JSONL).
func readLastAssistantMessage(path string, maxBytes int64) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	start := int64(0)
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", false, err
	}
	// Skip partial first line when we seek into the middle of a line.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if start > 0 {
		scanner.Scan()
	}

	var lastText string
	var lastHasToolUse bool
	var seenAssistant bool

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		text, hasTool, ok := extractAssistantTurn(line)
		if !ok {
			continue
		}
		lastText = text
		lastHasToolUse = hasTool
		seenAssistant = true
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	if !seenAssistant {
		return "", false, nil
	}
	return lastText, lastHasToolUse, nil
}

// extractAssistantTurn parses one JSONL line and, if it represents an
// assistant turn, returns the joined text and whether it contains any
// tool_use block. Supports both flat and nested Claude Code transcript
// shapes so we don't break across minor schema revisions.
func extractAssistantTurn(line []byte) (string, bool, bool) {
	var envelope struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Message *transcriptMsg  `json:"message,omitempty"`
		Content json.RawMessage `json:"content,omitempty"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return "", false, false
	}
	role := envelope.Role
	var contentRaw json.RawMessage
	if envelope.Message != nil {
		if envelope.Message.Role != "" {
			role = envelope.Message.Role
		}
		contentRaw = envelope.Message.Content
	}
	if len(envelope.Content) > 0 {
		contentRaw = envelope.Content
	}
	if envelope.Type != "" && envelope.Type != "assistant" && role == "" {
		return "", false, false
	}
	if role != "" && role != "assistant" {
		return "", false, false
	}
	if envelope.Type != "" && envelope.Type != "assistant" && role != "assistant" {
		return "", false, false
	}
	if len(contentRaw) == 0 {
		return "", false, false
	}
	var blocks []transcriptContentBlock
	if err := json.Unmarshal(contentRaw, &blocks); err != nil {
		return "", false, false
	}
	var buf strings.Builder
	hasTool := false
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if buf.Len() > 0 {
				buf.WriteByte('\n')
			}
			buf.WriteString(b.Text)
		case "tool_use":
			hasTool = true
		}
	}
	return buf.String(), hasTool, true
}

type transcriptMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type transcriptContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// containsAnyTag reports whether text contains any of tags as a whole word,
// case-insensitive. Brackets and punctuation count as word boundaries, so
// [COMPLETION], COMPLETION, and completion all match "COMPLETION". A tag
// that is a substring of a longer alphanumeric run (COMPLETIONS, RECOMPLETED)
// does NOT match — the loop relies on Claude writing the tag standalone.
func containsAnyTag(text string, tags []string) bool {
	if text == "" || len(tags) == 0 {
		return false
	}
	upper := strings.ToUpper(text)
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if wordMatch(upper, strings.ToUpper(tag)) {
			return true
		}
	}
	return false
}

func wordMatch(text, tag string) bool {
	idx := 0
	for {
		p := strings.Index(text[idx:], tag)
		if p < 0 {
			return false
		}
		pos := idx + p
		leftOK := pos == 0 || !isWordChar(text[pos-1])
		endPos := pos + len(tag)
		rightOK := endPos == len(text) || !isWordChar(text[endPos])
		if leftOK && rightOK {
			return true
		}
		idx = pos + 1
	}
}

func isWordChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}
