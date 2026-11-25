package checkselect

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestStdioMCPUsesRecommendationSnapshot(t *testing.T) {
	stages, err := PackDemo()
	if err != nil {
		t.Fatal(err)
	}
	s := stages["updated"].Snapshot
	const meta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`
	const args = `"arguments":{"changes":[{"status":"M","path":"api/routes.go"}]}`
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + meta + `}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{` + meta + `}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"recommend",` + args + `,` + meta + `}}`,
		`{"jsonrpc":"2.0","id":4,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"explain",` + args + `}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"recommend","arguments":{"repo":"/","changes":[]}}}`,
	}
	var out bytes.Buffer
	if err := ServeMCP(strings.NewReader(strings.Join(requests, "\n")), &out, s); err != nil {
		t.Fatal(err)
	}
	var results []json.RawMessage
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		var result json.RawMessage
		if err := json.Unmarshal(line, &result); err != nil {
			t.Fatal(err)
		}
		results = append(results, result)
	}
	if len(results) != 6 {
		t.Fatalf("wanted six replies and no notification reply; got %d", len(results))
	}
	for i, want := range []string{"supportedVersions", "readOnlyHint", "integration-smoke", mcpLegacy,
		stages["updated"].Receipt.SnapshotSHA256, "-32602"} {
		if !bytes.Contains(results[i], []byte(want)) {
			t.Errorf("response %d lacks %q: %s", i+1, want, results[i])
		}
	}
}
