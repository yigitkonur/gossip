// Package mcp implements a minimal stdio MCP server hand-rolled to support
// Claude Code's experimental notifications/claude/channel push extension.
package mcp

import "encoding/json"

// Request is an inbound MCP request from Claude Code.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a reply to an inbound request.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// ResponseError is the standard JSON-RPC error object.
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Notification is an outbound server-to-client notification with an arbitrary method.
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// InitializeResult is the response to the initialize request.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	Instructions    string             `json:"instructions,omitempty"`
}

// ServerInfo identifies this server to the client.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerCapabilities advertises what this server supports.
type ServerCapabilities struct {
	Experimental map[string]struct{} `json:"experimental,omitempty"`
	Tools        struct{}            `json:"tools"`
}

// ChannelNotificationParams is the payload of notifications/claude/channel.
type ChannelNotificationParams struct {
	Content string            `json:"content"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// Tool is a single tool advertised via tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolListResult is the result body of a tools/list response.
type ToolListResult struct {
	Tools []Tool `json:"tools"`
}

// ToolCallResult is the result body of a tools/call response.
type ToolCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a single content item in a tool call result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
