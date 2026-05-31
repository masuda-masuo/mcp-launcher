package mcpproxy

import (
	"bytes"
	"encoding/json"
)

// sentinelID is the JSON-RPC id (as raw JSON) used when replaying the cached
// initialize request to a freshly spawned child. It is namespaced so it can
// never collide with a client-issued id, which lets the proxy reliably swallow
// the corresponding response instead of forwarding it to the client.
const sentinelID = `"__mcp_launcher_init__"`

// Message is a just-enough parse of a single newline-delimited JSON-RPC 2.0
// message together with its exact framing bytes.
//
// MCP stdio frames are line-delimited JSON. We deliberately avoid fully
// decoding the payload: the proxy only needs the method and id to route a
// message, and keeping Raw lets us forward bytes unchanged.
type Message struct {
	Raw    []byte // exact bytes as read, including the trailing newline when present
	method string
	id     json.RawMessage // nil when the message has no id
	hasID  bool
}

type wireMessage struct {
	Method *string         `json:"method"`
	ID     json.RawMessage `json:"id"`
}

// Parse sniffs the method and id out of a single JSON-RPC message. The returned
// Message retains the original bytes in Raw.
func Parse(raw []byte) (Message, error) {
	m := Message{Raw: raw}
	var w wireMessage
	if err := json.Unmarshal(bytes.TrimRight(raw, "\r\n"), &w); err != nil {
		return m, err
	}
	if w.Method != nil {
		m.method = *w.Method
	}
	if len(w.ID) > 0 && !bytes.Equal(bytes.TrimSpace(w.ID), []byte("null")) {
		m.id = w.ID
		m.hasID = true
	}
	return m, nil
}

// Method returns the JSON-RPC method, or "" for responses.
func (m Message) Method() string { return m.method }

// HasID reports whether the message carried a non-null id.
func (m Message) HasID() bool { return m.hasID }

// IDRaw returns the raw JSON bytes of the id (nil when absent).
func (m Message) IDRaw() json.RawMessage { return m.id }

// IDKey returns a canonical map-key form of the id ("" when absent).
//
// The key is the raw JSON of the id, so numeric id 42 (key "42") and string id
// "42" (key "\"42\"") never collide — matching JSON-RPC equality semantics.
func (m Message) IDKey() string {
	if !m.hasID {
		return ""
	}
	return string(m.id)
}

// IsRequest reports a method call expecting a response (method + id).
func (m Message) IsRequest() bool { return m.method != "" && m.hasID }

// IsNotification reports a one-way message (method, no id).
func (m Message) IsNotification() bool { return m.method != "" && !m.hasID }

// IsResponse reports a result/error reply (id, no method).
func (m Message) IsResponse() bool { return m.method == "" && m.hasID }

// setID returns the message bytes with the "id" field replaced by newID. Keys
// may be reordered (irrelevant for JSON-RPC). A trailing newline is appended.
func setID(raw []byte, newID json.RawMessage) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimRight(raw, "\r\n"), &obj); err != nil {
		return nil, err
	}
	obj["id"] = newID
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// errorResponse builds a JSON-RPC error response for the given raw id.
func errorResponse(id json.RawMessage, code int, message string) []byte {
	type rpcError struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	resp := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   rpcError        `json:"error"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Error:   rpcError{Code: code, Message: message},
	}
	b, _ := json.Marshal(resp)
	return append(b, '\n')
}

// extractProtocolVersion pulls result.protocolVersion from an initialize
// response, returning "" when absent.
func extractProtocolVersion(raw []byte) string {
	var r struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	_ = json.Unmarshal(bytes.TrimRight(raw, "\r\n"), &r)
	return r.Result.ProtocolVersion
}
