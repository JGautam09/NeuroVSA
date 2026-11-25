package checkselect

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
)

const (
	mcpModern = "2026-07-28"
	mcpLegacy = "2025-11-25"
)

var rpcInteger = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ServeMCP provides a read-only, newline-delimited stdio adapter. The caller
// chooses the snapshot at startup. Tool arguments are structured changes only.
func ServeMCP(input io.Reader, output io.Writer, snapshot Snapshot) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	encoder := json.NewEncoder(output)
	initialized, ready := false, false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		response := map[string]any{"jsonrpc": "2.0", "id": nil}
		var result map[string]any
		var failure *rpcError
		switch err := json.Unmarshal(line, &req); {
		case !json.Valid(line):
			failure = &rpcError{Code: -32700, Message: "Parse error"}
		case err != nil || req.JSONRPC != "2.0" || req.Method == "":
			failure = &rpcError{Code: -32600, Message: "Invalid request"}
		case len(req.ID) == 0:
			if initialized && req.Method == "notifications/initialized" {
				ready = true
			}
			continue
		case !validRPCID(req.ID):
			failure = &rpcError{Code: -32600, Message: "Invalid request ID"}
		default:
			response["id"] = req.ID
			result, failure = mcpRequest(req, snapshot, &initialized, &ready)
		}
		if failure != nil {
			response["error"] = failure
		} else {
			response["result"] = result
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func validRPCID(id json.RawMessage) bool {
	return len(id) > 0 && (id[0] == '"' || rpcInteger.Match(id))
}

func mcpRequest(req rpcRequest, snapshot Snapshot, initialized, ready *bool) (map[string]any, *rpcError) {
	info := map[string]string{"name": "nvsa-check", "version": "1.0.0"}
	capabilities := map[string]any{"tools": map[string]bool{"listChanged": false}}
	params := map[string]json.RawMessage{}
	if len(req.Params) > 0 && (json.Unmarshal(req.Params, &params) != nil || params == nil) {
		return nil, &rpcError{Code: -32602, Message: "Expected object parameters"}
	}
	var meta map[string]json.RawMessage
	_ = json.Unmarshal(params["_meta"], &meta)
	versionParam, modern := meta["io.modelcontextprotocol/protocolVersion"]
	if modern {
		var version string
		var clientCaps map[string]json.RawMessage
		if json.Unmarshal(versionParam, &version) != nil || version == "" ||
			json.Unmarshal(meta["io.modelcontextprotocol/clientCapabilities"], &clientCaps) != nil || clientCaps == nil {
			return nil, &rpcError{Code: -32602, Message: "Expected protocol version and client capabilities"}
		}
		if version != mcpModern {
			return nil, &rpcError{Code: -32022, Message: "Unsupported protocol version",
				Data: map[string]any{"supported": []string{mcpModern, mcpLegacy}, "requested": version}}
		}
	} else {
		if req.Method == "initialize" {
			var version string
			if json.Unmarshal(params["protocolVersion"], &version) != nil || version == "" {
				return nil, &rpcError{Code: -32602, Message: "Expected protocolVersion"}
			}
			*initialized, *ready = true, false
			return map[string]any{"protocolVersion": mcpLegacy, "serverInfo": info, "capabilities": capabilities}, nil
		}
		if !*ready && req.Method != "ping" {
			if req.Method == "server/discover" {
				return nil, &rpcError{Code: -32602, Message: "Expected per-request protocol metadata"}
			}
			return nil, &rpcError{Code: -32600, Message: "Initialize the legacy session first"}
		}
	}

	result := map[string]any{}
	switch req.Method {
	case "server/discover":
		if !modern {
			return nil, &rpcError{Code: -32601, Message: "Discovery requires modern protocol metadata"}
		}
		result["supportedVersions"] = []string{mcpModern}
		result["capabilities"] = capabilities
	case "ping":
	case "tools/list":
		result["tools"] = checkTools()
	case "tools/call":
		value, failure := callCheckTool(params, snapshot)
		if failure != nil {
			return nil, failure
		}
		result = value
	default:
		return nil, &rpcError{Code: -32601, Message: "Method not found"}
	}
	if modern {
		result["resultType"] = "complete"
		result["_meta"] = map[string]any{"io.modelcontextprotocol/serverInfo": info}
	}
	return result, nil
}

func checkTools() []map[string]any {
	// Paths are labels for policy matching. The tools never open the paths.
	schema := json.RawMessage(`{"type":"object","required":["changes"],"additionalProperties":false,
		"properties":{"changes":{"type":"array","maxItems":256,"items":{
		"type":"object","required":["status","path"],"additionalProperties":false,
		"properties":{"status":{"enum":["A","C","D","M","R","T"]},
		"path":{"type":"string"},"old_path":{"type":"string"}}}}}}`)
	return []map[string]any{
		{"name": "explain", "description": "Explain checks for supplied changes and return the snapshot digest.",
			"inputSchema": schema, "annotations": map[string]bool{"readOnlyHint": true, "openWorldHint": false}},
		{"name": "recommend", "description": "Recommend approved check IDs for supplied repository changes.",
			"inputSchema": schema, "annotations": map[string]bool{"readOnlyHint": true, "openWorldHint": false}},
	}
}

func callCheckTool(params map[string]json.RawMessage, s Snapshot) (map[string]any, *rpcError) {
	var name string
	if json.Unmarshal(params["name"], &name) != nil || (name != "recommend" && name != "explain") {
		return nil, &rpcError{Code: -32602, Message: "Unknown tool name"}
	}
	var args struct {
		Changes []Change `json:"changes"`
	}
	err := decodeJSON(params["arguments"], &args)
	if err != nil || args.Changes == nil || len(args.Changes) > 256 {
		return nil, &rpcError{Code: -32602, Message: "Provide a changes array with at most 256 entries"}
	}
	replay, err := s.Evaluate(args.Changes, false)
	if err != nil {
		return nil, &rpcError{Code: -32602, Message: fmt.Sprintf("Invalid changes: %v", err)}
	}
	var value any = replay.Receipt.Recommendation
	if name == "explain" {
		value = replay.Receipt
	}
	data, _ := json.Marshal(value)
	return map[string]any{
		"content":           []map[string]string{{"type": "text", "text": string(data)}},
		"structuredContent": value,
	}, nil
}
