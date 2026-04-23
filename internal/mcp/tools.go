package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yigitkonur/gossip/internal/protocol"
)

var consultCodexInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "text": {"type": "string", "description": "The message to send to Codex."},
    "require_reply": {"type": "boolean", "description": "When true, asks Codex to mark its reply [IMPORTANT]. The tool call itself returns as soon as the message is injected — the reply arrives asynchronously via the next get_messages call (pull mode) or a channel tag (push mode)."}
  },
  "required": ["text"]
}`)

var getMessagesInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {},
  "required": []
}`)

const consultCodexDescription = "Send a message to Codex — lands as a new user turn in its session. Use any time you want Codex to act, verify, or answer. Available whenever gossip is running; no prior Codex message required. Prefer ending your turn with [COMPLETION] on its own line for the automatic review loop; reserve this tool for targeted mid-turn consultations."

const getMessagesDescription = "Drain pending messages from Codex. Call when you expect a Codex response or want to check what has accumulated."

func (s *Server) handleToolsList(req Request) {
	result := ToolListResult{
		Tools: []Tool{
			{Name: "consult_codex", Description: consultCodexDescription, InputSchema: consultCodexInputSchema},
			{Name: "get_messages", Description: getMessagesDescription, InputSchema: getMessagesInputSchema},
		},
	}
	s.respond(req.ID, result)
}

func (s *Server) handleToolsCall(ctx context.Context, req Request) {
	var call struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		s.respondError(req.ID, -32602, "invalid tools/call params: "+err.Error())
		return
	}
	switch call.Name {
	case "consult_codex":
		s.handleConsultCodex(ctx, req.ID, call.Arguments)
	case "get_messages":
		s.handleGetMessagesTool(req.ID)
	default:
		s.respondError(req.ID, -32601, "unknown tool: "+call.Name)
	}
}

func (s *Server) handleConsultCodex(ctx context.Context, reqID json.RawMessage, args map[string]any) {
	text, _ := args["text"].(string)
	if text == "" {
		s.respond(reqID, ToolCallResult{IsError: true, Content: []ToolContent{{Type: "text", Text: "Error: missing required parameter 'text'"}}})
		return
	}
	requireReply, _ := args["require_reply"].(bool)
	if s.opts.ReplyHandler == nil {
		s.respond(reqID, ToolCallResult{IsError: true, Content: []ToolContent{{Type: "text", Text: "Error: bridge not initialized, cannot send."}}})
		return
	}
	msgID := fmt.Sprintf("consult_%d", time.Now().UnixMilli())
	result := s.opts.ReplyHandler(ctx, protocol.BridgeMessage{
		ID:        msgID,
		Source:    protocol.SourceClaude,
		Content:   text,
		Timestamp: time.Now().UnixMilli(),
	}, requireReply)
	if !result.Success {
		s.respond(reqID, ToolCallResult{IsError: true, Content: []ToolContent{{Type: "text", Text: "Error: " + result.Error}}})
		return
	}
	response := "Sent."
	if pending := s.pendingCount(); pending > 0 {
		response += fmt.Sprintf(" %d Codex message(s) in your inbox.", pending)
	}
	s.respond(reqID, ToolCallResult{Content: []ToolContent{{Type: "text", Text: response}}})
}

func (s *Server) handleGetMessagesTool(reqID json.RawMessage) {
	messages, dropped := s.drainQueue()
	if len(messages) == 0 && dropped == 0 {
		s.respond(reqID, ToolCallResult{Content: []ToolContent{{Type: "text", Text: "No new messages from Codex."}}})
		return
	}
	count := len(messages)
	plural := ""
	if count != 1 {
		plural = "s"
	}
	var header strings.Builder
	header.WriteString(fmt.Sprintf("[%d new message%s from Codex]", count, plural))
	if dropped > 0 {
		header.WriteString(fmt.Sprintf("\n(%d older message(s) were dropped due to queue overflow)", dropped))
	}
	header.WriteString(fmt.Sprintf("\nchat_id: %s", s.chatID()))
	var body string
	for i, m := range messages {
		body += fmt.Sprintf("\n---\n[%d] %s\nCodex: %s", i+1, time.UnixMilli(m.Timestamp).Format(time.RFC3339), m.Content)
	}
	s.respond(reqID, ToolCallResult{Content: []ToolContent{{Type: "text", Text: header.String() + body}}})
}

func (s *Server) drainQueue() ([]protocol.BridgeMessage, int) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	msgs := s.queue
	dropped := s.droppedMessages
	if s.opts.DroppedCountProvider != nil {
		externalDropped := s.opts.DroppedCountProvider()
		if externalDropped > s.lastExternalDropped {
			dropped += externalDropped - s.lastExternalDropped
		}
		s.lastExternalDropped = externalDropped
	}
	s.queue = nil
	s.droppedMessages = 0
	return msgs, dropped
}

func (s *Server) pendingCount() int {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	return len(s.queue)
}
