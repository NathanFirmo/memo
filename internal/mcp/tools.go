package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/NathanFirmo/memo/internal/embed"
	"github.com/NathanFirmo/memo/internal/store"
)

var ToolNames = []string{
	"memo_add_memory",
	"memo_search_memory",
	"memo_memory_stats",
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func Serve(ctx context.Context, s *store.Store, out io.Writer, in io.Reader) error {
	if in == nil {
		in = os.Stdin
	}
	scanner := bufio.NewScanner(in)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			_ = encoder.Encode(errorResponse(nil, -32700, err.Error()))
			continue
		}
		resp := handle(ctx, s, req)
		if req.ID != nil {
			if err := encoder.Encode(resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func handle(ctx context.Context, s *store.Store, req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return ok(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]any{"name": "memo", "version": "0.1.0"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		})
	case "tools/list":
		return ok(req.ID, map[string]any{"tools": toolDefinitions()})
	case "tools/call":
		result, err := callTool(ctx, s, req.Params)
		if err != nil {
			return errorResponse(req.ID, -32000, err.Error())
		}
		return ok(req.ID, result)
	case "notifications/initialized":
		return ok(req.ID, map[string]any{})
	default:
		return errorResponse(req.ID, -32601, "method not found")
	}
}

func callTool(ctx context.Context, s *store.Store, raw json.RawMessage) (any, error) {
	var req struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	args := map[string]any{}
	if len(req.Arguments) > 0 {
		if err := json.Unmarshal(req.Arguments, &args); err != nil {
			return nil, err
		}
	}

	switch req.Name {
	case "memo_add_memory":
		title := stringArg(args, "title", "")
		body := stringArg(args, "body", "")
		vector := embedIfPossible(ctx, s, title+"\n"+body)
		id, err := s.AddMemory(ctx, store.MemoryInput{
			Title: title,
			Body:  body,
		}, vector)
		if err != nil {
			return nil, err
		}
		return textResult(fmt.Sprintf("memory added: %d", id)), nil
	case "memo_search_memory":
		query := stringArg(args, "query", "")
		vector := embedIfPossible(ctx, s, query)
		results, err := s.Search(ctx, store.SearchOptions{
			Query:          query,
			Limit:          intArg(args, "limit", 10),
			Vector:         vector,
			MinVectorScore: floatArg(args, "min_vector_score", 0),
		})
		if err != nil {
			return nil, err
		}
		data, _ := json.Marshal(results)
		return textResult(string(data)), nil
	case "memo_memory_stats":
		stats, err := s.Stats(ctx)
		if err != nil {
			return nil, err
		}
		data, _ := json.Marshal(stats)
		return textResult(string(data)), nil
	default:
		return nil, fmt.Errorf("unknown tool %q", req.Name)
	}
}

func embedIfPossible(ctx context.Context, s *store.Store, text string) []float32 {
	vector, err := embed.NewClient("", "").Embed(ctx, text)
	if err != nil || len(vector) == 0 {
		return nil
	}
	_ = s.EnsureVectorTable(len(vector))
	return vector
}

func toolDefinitions() []map[string]any {
	tools := make([]map[string]any, 0, len(ToolNames))
	for _, name := range ToolNames {
		tools = append(tools, map[string]any{
			"name":        name,
			"description": description(name),
			"inputSchema": map[string]any{"type": "object", "additionalProperties": true},
		})
	}
	return tools
}

func description(name string) string {
	switch name {
	case "memo_add_memory":
		return "Store a durable local memory."
	case "memo_search_memory":
		return "Search local memories."
	case "memo_memory_stats":
		return "Return memory database statistics."
	default:
		return name
	}
}

func textResult(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func ok(id any, result any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id any, code int, message string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

func stringArg(args map[string]any, key, fallback string) string {
	if value, ok := args[key].(string); ok {
		return value
	}
	return fallback
}

func intArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return fallback
	}
}

func floatArg(args map[string]any, key string, fallback float64) float64 {
	if value, ok := args[key].(float64); ok {
		return value
	}
	return fallback
}
