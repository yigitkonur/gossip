package mcp

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/yigitkonur/gossip/internal/protocol"
)

// PushMessage delivers a Codex message to Claude.
func (s *Server) PushMessage(msg protocol.BridgeMessage) {
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}
	if s.opts.DeliveryMode == DeliveryPush {
		if s.bufferPushBeforeServe(msg) {
			return
		}
		s.pushViaChannel(msg)
		return
	}
	s.queueForPull(msg)
}

func (s *Server) pushViaChannel(msg protocol.BridgeMessage) {
	seq := s.notificationSeq.Add(1)
	msgID := fmt.Sprintf("codex_msg_%s_%d", s.sessionID, seq)
	ts := time.UnixMilli(msg.Timestamp).UTC().Format(time.RFC3339)
	params := ChannelNotificationParams{
		Content: msg.Content,
		Meta: map[string]string{
			"chat_id":     s.sessionID,
			"message_id":  msgID,
			"user":        "Codex",
			"user_id":     "codex",
			"ts":          ts,
			"source_type": "codex",
		},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		s.log("channel notification marshal: " + err.Error())
		return
	}
	if err := s.write(Notification{JSONRPC: "2.0", Method: "notifications/claude/channel", Params: raw}); err != nil {
		s.queueForPull(msg)
	}
}

func (s *Server) queueForPull(msg protocol.BridgeMessage) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if len(s.queue) >= s.opts.MaxBufferedMessages {
		s.queue = s.queue[1:]
		s.droppedMessages++
	}
	s.queue = append(s.queue, msg)
}

func (s *Server) bufferPushBeforeServe(msg protocol.BridgeMessage) bool {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writer != nil {
		return false
	}
	if len(s.preServePush) >= s.opts.MaxBufferedMessages {
		s.preServePush = s.preServePush[1:]
		s.queueMu.Lock()
		s.droppedMessages++
		s.queueMu.Unlock()
	}
	s.preServePush = append(s.preServePush, msg)
	return true
}
