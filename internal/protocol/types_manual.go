package protocol

import "encoding/json"

// ThreadStartParams is the thread/start request payload.
type ThreadStartParams struct {
	Cwd string `json:"cwd,omitempty"`
}

// ThreadStartResponse is returned by thread/start.
type ThreadStartResponse struct {
	Thread Thread `json:"thread"`
}

// Thread is the minimal thread identity shape AgentBridge tracks.
type Thread struct {
	ID string `json:"id"`
}

// TurnStartParams is the turn/start request payload.
type TurnStartParams struct {
	ThreadID string      `json:"threadId"`
	Input    []UserInput `json:"input"`
}

// UserInput is one user-provided turn input item.
type UserInput struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// TurnStartedParams is the turn/started notification payload.
type TurnStartedParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

// TurnCompletedParams is the turn/completed notification payload.
type TurnCompletedParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

// AgentMessageDeltaParams is the item/agentMessage/delta notification payload.
type AgentMessageDeltaParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

// ItemStartedParams is the item/started notification payload.
type ItemStartedParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Item     Item   `json:"item"`
}

// ItemCompletedParams is the item/completed notification payload.
type ItemCompletedParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Item     Item   `json:"item"`
}

// ItemContent is one content fragment inside an item.
type ItemContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Item is a generic content item with raw variant content available on demand.
type Item struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Content    []ItemContent   `json:"content,omitempty"`
	RawContent json.RawMessage `json:"-"`
}

// InitializeParams is the initialize request payload.
type InitializeParams struct {
	ClientInfo ClientInfo `json:"clientInfo"`
}

// ClientInfo identifies the connecting client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
