package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Signal relay: the OPTIONAL auto-connect path for RuleGarden live sync. It pairs exactly
// two peers per room code and forwards opaque text frames between them — in practice the
// same base64 offer/answer blobs the manual copy/paste flow uses, so the relay replaces
// the copy/paste gesture and nothing else.
//
// Privacy, stated plainly: a signaling server necessarily sees connection metadata (SDP
// carries addresses — that is what signaling is), but it never sees world data. Lessons
// flow over the resulting WebRTC data channel, peer to peer. Manual copy/paste signaling
// remains the serverless default; this endpoint is for players who prefer a room code.
//
// Wire protocol (text frames):
//   - server → client control frames are JSON objects: {"type":"role","role":"host"|"join"},
//     {"type":"peer-joined"}, {"type":"peer-left"}, {"type":"error","error":"..."}
//   - everything a client SENDS is relayed verbatim to its room peer. Clients send bare
//     base64 blobs, which can never start with '{', so control and relay frames are
//     unambiguous by first byte.
const (
	// MaxSignalFrameBytes bounds one relayed frame. Offer/answer blobs are ~2 KB;
	// 64 KiB is generous headroom, and anything larger is not signaling.
	MaxSignalFrameBytes = 64 << 10
	// MaxSignalRooms caps concurrent rooms — this is a small game relay, not infrastructure.
	MaxSignalRooms = 256
	// signalRoomTTL evicts rooms whose host never got a peer (lazy sweep on new connects).
	signalRoomTTL = 5 * time.Minute
)

// signalConn serializes writes: gorilla/websocket permits only one concurrent writer per
// connection, and a room peer's control frames and relayed frames arrive from different
// goroutines (each side's read loop writes to the OTHER side). Reads stay single-goroutine.
type signalConn struct {
	wmu sync.Mutex
	c   *websocket.Conn
}

func (sc *signalConn) writeText(msg []byte) {
	sc.wmu.Lock()
	defer sc.wmu.Unlock()
	_ = sc.c.WriteMessage(websocket.TextMessage, msg)
}

type signalRoom struct {
	created time.Time
	host    *signalConn
	join    *signalConn
}

type signalHub struct {
	mu    sync.Mutex
	rooms map[string]*signalRoom
}

func newSignalHub() *signalHub {
	return &signalHub{rooms: make(map[string]*signalRoom)}
}

func signalControl(sc *signalConn, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	sc.writeText(b)
}

func validRoomCode(room string) bool {
	if len(room) == 0 || len(room) > 64 {
		return false
	}
	for i := 0; i < len(room); i++ {
		c := room[i]
		ok := c == '-' || c == '_' ||
			(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}

// handle upgrades the connection and runs the room lifecycle. The same loopback-only
// origin policy as /ws applies (see checkOrigin / -allow-all-origins).
func (h *signalHub) handle(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if !validRoomCode(room) {
		http.Error(w, "invalid room code (1-64 chars: letters, digits, - or _)", http.StatusBadRequest)
		return
	}
	raw, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already replied
	}
	raw.SetReadLimit(MaxSignalFrameBytes)
	conn := &signalConn{c: raw}

	h.mu.Lock()
	h.sweepLocked()
	rm := h.rooms[room]
	var role string
	var peer *signalConn
	switch {
	case rm == nil:
		if len(h.rooms) >= MaxSignalRooms {
			h.mu.Unlock()
			signalControl(conn, map[string]string{"type": "error", "error": "relay is at capacity"})
			raw.Close()
			return
		}
		h.rooms[room] = &signalRoom{created: time.Now(), host: conn}
		role = "host"
	case rm.join == nil && rm.host != conn:
		rm.join = conn
		role = "join"
		peer = rm.host
	default:
		h.mu.Unlock()
		signalControl(conn, map[string]string{"type": "error", "error": fmt.Sprintf("room %q is full", room)})
		raw.Close()
		return
	}
	h.mu.Unlock()

	signalControl(conn, map[string]string{"type": "role", "role": role})
	if peer != nil {
		signalControl(peer, map[string]string{"type": "peer-joined"})
	}

	// Relay loop: forward every frame verbatim to whoever the room's other side is NOW
	// (the join side may attach after the host has already started reading).
	for {
		mt, msg, err := raw.ReadMessage()
		if err != nil {
			break
		}
		if mt != websocket.TextMessage {
			continue // signaling is text-only; drop anything else
		}
		h.mu.Lock()
		rm := h.rooms[room]
		var other *signalConn
		if rm != nil {
			if conn == rm.host {
				other = rm.join
			} else {
				other = rm.host
			}
		}
		h.mu.Unlock()
		if other != nil {
			other.writeText(msg)
		}
	}

	// Teardown: drop the room, tell the survivor.
	h.mu.Lock()
	var survivor *signalConn
	if rm := h.rooms[room]; rm != nil && (rm.host == conn || rm.join == conn) {
		if rm.host == conn {
			survivor = rm.join
		} else {
			survivor = rm.host
		}
		delete(h.rooms, room)
	}
	h.mu.Unlock()
	if survivor != nil {
		signalControl(survivor, map[string]string{"type": "peer-left"})
	}
	raw.Close()
}

// sweepLocked evicts stale never-paired rooms. Caller holds h.mu.
func (h *signalHub) sweepLocked() {
	cutoff := time.Now().Add(-signalRoomTTL)
	for code, rm := range h.rooms {
		if rm.join == nil && rm.created.Before(cutoff) {
			rm.host.c.Close()
			delete(h.rooms, code)
		}
	}
}
