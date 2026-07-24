package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func signalTestServer(t *testing.T) (*httptest.Server, func(room string) *websocket.Conn) {
	t.Helper()
	hub := newSignalHub()
	srv := httptest.NewServer(http.HandlerFunc(hub.handle))
	t.Cleanup(srv.Close)

	dial := func(room string) *websocket.Conn {
		t.Helper()
		url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/?room=" + room
		hdr := http.Header{"Origin": []string{"http://127.0.0.1"}} // loopback passes checkOrigin
		c, _, err := websocket.DefaultDialer.Dial(url, hdr)
		if err != nil {
			t.Fatalf("dial room %q: %v", room, err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	return srv, dial
}

func readFrame(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(msg)
}

func expectControl(t *testing.T, c *websocket.Conn, wantType string) map[string]string {
	t.Helper()
	frame := readFrame(t, c)
	var m map[string]string
	if err := json.Unmarshal([]byte(frame), &m); err != nil || m["type"] != wantType {
		t.Fatalf("want control %q, got %q", wantType, frame)
	}
	return m
}

// TestSignalRelayPairsAndForwards is the whole auto-connect contract: role assignment,
// join notification, verbatim relay both ways, and peer-left on disconnect.
func TestSignalRelayPairsAndForwards(t *testing.T) {
	_, dial := signalTestServer(t)

	host := dial("garden-1")
	if m := expectControl(t, host, "role"); m["role"] != "host" {
		t.Fatalf("first peer must be host, got %v", m)
	}
	join := dial("garden-1")
	if m := expectControl(t, join, "role"); m["role"] != "join" {
		t.Fatalf("second peer must be join, got %v", m)
	}
	expectControl(t, host, "peer-joined")

	// Verbatim relay in both directions (blobs are opaque to the relay).
	if err := host.WriteMessage(websocket.TextMessage, []byte("OFFER-BLOB")); err != nil {
		t.Fatal(err)
	}
	if got := readFrame(t, join); got != "OFFER-BLOB" {
		t.Fatalf("join received %q, want the verbatim offer", got)
	}
	if err := join.WriteMessage(websocket.TextMessage, []byte("ANSWER-BLOB")); err != nil {
		t.Fatal(err)
	}
	if got := readFrame(t, host); got != "ANSWER-BLOB" {
		t.Fatalf("host received %q, want the verbatim answer", got)
	}

	// Disconnect one side: the survivor is told, the room dies.
	join.Close()
	expectControl(t, host, "peer-left")
}

func TestSignalRoomFullAndReuse(t *testing.T) {
	_, dial := signalTestServer(t)

	host := dial("busy")
	expectControl(t, host, "role")
	join := dial("busy")
	expectControl(t, join, "role")
	expectControl(t, host, "peer-joined")

	third := dial("busy")
	if m := expectControl(t, third, "error"); !strings.Contains(m["error"], "full") {
		t.Fatalf("third peer must be refused, got %v", m)
	}

	// After both leave, the code is reusable.
	host.Close()
	expectControl(t, join, "peer-left")
	join.Close()
	time.Sleep(50 * time.Millisecond) // let teardown run
	again := dial("busy")
	if m := expectControl(t, again, "role"); m["role"] != "host" {
		t.Fatalf("reused room must start fresh as host, got %v", m)
	}
}

func TestSignalRejectsBadRoomCodes(t *testing.T) {
	hub := newSignalHub()
	srv := httptest.NewServer(http.HandlerFunc(hub.handle))
	defer srv.Close()

	for _, room := range []string{"", strings.Repeat("x", 65), "has space", "sl/ash", "q?m"} {
		resp, err := http.Get(srv.URL + "/?room=" + strings.ReplaceAll(room, " ", "%20"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("room %q: status %d, want 400", room, resp.StatusCode)
		}
	}
}

func TestSignalOversizedFrameDisconnects(t *testing.T) {
	_, dial := signalTestServer(t)
	host := dial("big")
	expectControl(t, host, "role")
	join := dial("big")
	expectControl(t, join, "role")
	expectControl(t, host, "peer-joined")

	huge := strings.Repeat("A", MaxSignalFrameBytes+1)
	_ = host.WriteMessage(websocket.TextMessage, []byte(huge))
	// The relay must drop the offender (read limit), notifying the peer — and must NOT
	// forward the oversized frame.
	if m := expectControl(t, join, "peer-left"); m["type"] != "peer-left" {
		t.Fatalf("peer should see peer-left after oversized frame, got %v", m)
	}
}
