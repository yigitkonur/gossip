package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yigitkonur/gossip/internal/protocol"
)

var replyInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "chat_id": {"type": "string", "description": "The conversation to reply in (from the inbound <channel> tag)."},
    "text": {"type": "string", "description": "The message to send to Codex."},
    "require_reply": {"type": "boolean", "description": "When true, Codex is required to send a reply."}
  },
  "required": ["text"]
}`)

var getMessagesInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {},
  "required": []
}`)

func (s *Server) handleToolsList(req Request) {
	result := ToolListResult{
		Tools: []Tool{
			{Name: "reply", Description: "Send a message back to Codex. Your reply will be injected into the Codex session as a new user turn.", InputSchema: replyInputSchema},
			{Name: "get_messages", Description: "Check for new messages from Codex. Call this after sending a reply or when you expect a response from Codex.", InputSchema: getMessagesInputSchema},
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
	case "reply":
		s.handleReplyTool(ctx, req.ID, call.Arguments)
	case "get_messages":
		s.handleGetMessagesTool(req.ID)
	default:
		s.respondError(req.ID, -32601, "unknown tool: "+call.Name)
	}
}

func (s *Server) handleReplyTool(ctx context.Context, reqID json.RawMessage, args map[string]any) {
	text, _ := args["text"].(string)
	if text == "" {
		s.respond(reqID, ToolCallResult{IsError: true, Content: []ToolContent{{Type: "text", Text: "Error: missing required parameter 'text'"}}})
		return
	}
	chatID, _ := args["chat_id"].(string)
	requireReply, _ := args["require_reply"].(bool)
	if s.opts.ReplyHandler == nil {
		s.respond(reqID, ToolCallResult{IsError: true, Content: []ToolContent{{Type: "text", Text: "Error: bridge not initialized, cannot send reply."}}})
		return
	}
	msgID := chatID
	if msgID == "" {
		msgID = fmt.Sprintf("reply_%d", time.Now().UnixMilli())
	}
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
	response := "Reply sent to Codex."
	if pending := s.pendingCount(); pending > 0 {
		response += fmt.Sprintf(" Note: %d unread Codex message(s) waiting — call get_messages.", pending)
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
	header := fmt.Sprintf("[%d new message%s from Codex]", count, plural)
	if dropped > 0 {
		header += fmt.Sprintf(" (%d older message(s) were dropped due to queue overflow)", dropped)
	}
	var body string
	for i, m := range messages {
		body += fmt.Sprintf("\n---\n[%d] %s\nCodex: %s", i+1, time.UnixMilli(m.Timestamp).Format(time.RFC3339), m.Content)
	}
	s.respond(reqID, ToolCallResult{Content: []ToolContent{{Type: "text", Text: header + body}}})
}

func (s *Server) drainQueue() ([]protocol.BridgeMessage, int) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	msgs := s.queue
	dropped := s.droppedMessages
	s.queue = nil
	s.droppedMessages = 0
	return msgs, dropped
}

func (s *Server) pendingCount() int {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	return len(s.queue)
}
