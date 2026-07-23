package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/JGautam09/NeuroVSA/engine"
)

// dialTestServer wires the WebSocket handler into an httptest server and returns a connected
// client plus a cleanup func. The gorilla dialer sends no Origin header, so it passes the
// loopback-only origin check.
func dialTestServer(t *testing.T) (*websocket.Conn, func()) {
	t.Helper()
	s := NewServer(0)
	ts := httptest.NewServer(http.HandlerFunc(s.handleWebSocket))
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		ts.Close()
		t.Fatalf("dial: %v", err)
	}
	return conn, func() { conn.Close(); ts.Close() }
}

// A multi-word seed must now stream the correct continuation and then stop (no noise padding).
func TestServerPromptFlow(t *testing.T) {
	conn, cleanup := dialTestServer(t)
	defer cleanup()

	if err := conn.WriteJSON(ClientMessage{Type: "prompt", Text: "func main"}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var tokens []string
	for {
		var resp ServerResponse
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("read: %v", err)
		}
		switch resp.Type {
		case "token":
			tokens = append(tokens, resp.Value)
		case "done":
			if got, want := strings.Join(tokens, " "), "fmt.Println return nil"; got != want {
				t.Errorf("prompt 'func main' -> %q, want %q", got, want)
			}
			return
		case "error":
			t.Fatalf("server error: %s", resp.Error)
		}
	}
}

// Routing must be goal-dependent: different goals return different, correct first tools.
func TestServerRoutingFlow(t *testing.T) {
	conn, cleanup := dialTestServer(t)
	defer cleanup()

	route := func(goal string) string {
		if err := conn.WriteJSON(ClientMessage{Type: "route_tool", Goal: goal}); err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var resp ServerResponse
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("read: %v", err)
		}
		if resp.Type != "trajectory" {
			t.Fatalf("expected trajectory, got %q (%s)", resp.Type, resp.Error)
		}
		return resp.Action
	}

	if got := route("fix_bug"); got != engine.ToolASTSearch {
		t.Errorf("fix_bug first tool = %q, want %q", got, engine.ToolASTSearch)
	}
	if got := route("deploy_service"); got != engine.ToolRunTests {
		t.Errorf("deploy_service first tool = %q, want %q", got, engine.ToolRunTests)
	}
}

func TestServerASTFlow(t *testing.T) {
	conn, cleanup := dialTestServer(t)
	defer cleanup()

	if err := conn.WriteJSON(ClientMessage{Type: "index_ast", Path: "."}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	var resp ServerResponse
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "ast_done" {
		t.Fatalf("expected ast_done, got %q (%s)", resp.Type, resp.Error)
	}
}

// Tracing over the wire: token frames must carry candidate tables and ledger provenance, and
// the done frame must name the stop reason.
func TestServerTracedPromptFlow(t *testing.T) {
	conn, cleanup := dialTestServer(t)
	defer cleanup()

	if err := conn.WriteJSON(ClientMessage{Type: "prompt", Text: "func", Trace: true}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	sawToken := false
	for {
		var resp ServerResponse
		if err := conn.ReadJSON(&resp); err != nil {
			t.Fatalf("read: %v", err)
		}
		switch resp.Type {
		case "token":
			sawToken = true
			if resp.Trace == nil {
				t.Fatal("token frame missing trace payload")
			}
			if len(resp.Trace.Candidates) == 0 || resp.Trace.Candidates[0].Token != resp.Value {
				t.Fatalf("trace candidates malformed for token %q: %+v", resp.Value, resp.Trace.Candidates)
			}
			if len(resp.Trace.Contributors) == 0 {
				t.Fatalf("token %q trace has no ledger contributors", resp.Value)
			}
		case "done":
			if !sawToken {
				t.Fatal("no tokens streamed before done")
			}
			if resp.Summary == "" {
				t.Fatal("done frame missing stop reason")
			}
			return
		case "error":
			t.Fatalf("server error: %s", resp.Error)
		}
	}
}

// The AST indexer must be confined to its root: paths inside are allowed, absolute paths and
// parent-directory traversal are rejected.
func TestResolveIndexPathConfinement(t *testing.T) {
	s := NewServer(0)
	s.IndexRoot = "."

	for _, ok := range []string{"", ".", "core", "engine/sub"} {
		if _, err := s.resolveIndexPath(ok); err != nil {
			t.Errorf("expected %q to be allowed, got error: %v", ok, err)
		}
	}
	for _, bad := range []string{"/etc", "/etc/passwd", "..", "../..", "../../etc/passwd", "core/../.."} {
		if _, err := s.resolveIndexPath(bad); err == nil {
			t.Errorf("expected %q to be rejected, but it was allowed", bad)
		}
	}
}
