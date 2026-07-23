package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/JGautam09/NeuroVSA/core"
	"github.com/JGautam09/NeuroVSA/engine"
	"github.com/JGautam09/NeuroVSA/parser"
)

// AllowAllOrigins disables the default loopback-only WebSocket origin check. Leave it false
// for NeuroVSA's local / air-gapped posture; set it true (via the -allow-all-origins flag)
// only when intentionally serving the API to other hosts on a trusted network.
var AllowAllOrigins = false

var upgrader = websocket.Upgrader{
	CheckOrigin: checkOrigin,
}

// checkOrigin permits WebSocket upgrades only from loopback origins by default. A missing
// Origin header (non-browser clients such as curl or a native app) is allowed since there is
// no cross-site request to guard against.
func checkOrigin(r *http.Request) bool {
	if AllowAllOrigins {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// Server encapsulates the NeuroVSA WebSocket API server. All fields are shared across
// connections and are read-only or internally synchronized after construction; per-agent
// mutable routing state lives in a per-connection TrajectoryTracker (see handleWebSocket).
type Server struct {
	mu         sync.Mutex
	Port       int
	Dict       *core.TokenDictionary
	Memory     *engine.AssociativeMemory
	Decoder    *engine.HDCDecoder
	Router     *engine.ToolRouter
	ASTIndexer *parser.CodeASTIndexer
	// IndexRoot confines the /ast indexer: client-supplied paths are resolved against it and
	// may not escape it. Defaults to "." (the server's working directory).
	IndexRoot string
}

// ClientMessage represents incoming JSON packets from the React UI.
type ClientMessage struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Path string `json:"path,omitempty"`
	Goal string `json:"goal,omitempty"`
}

// ServerResponse represents outbound JSON packets streamed to the React UI.
type ServerResponse struct {
	Type    string `json:"type"`
	Value   string `json:"value,omitempty"`
	Dist    int    `json:"dist,omitempty"`
	Action  string `json:"action,omitempty"`
	Latency int64  `json:"latency_us,omitempty"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
}

// NewServer initializes a new API server instance.
func NewServer(port int) *Server {
	dict := core.NewTokenDictionary()
	mem := engine.NewAssociativeMemory()
	dec := engine.NewHDCDecoder(mem, dict)
	router := engine.NewToolRouter()
	indexer := parser.NewCodeASTIndexer(dict)

	// Populate initial dictionary with sample sequences for demonstration
	sampleTokens := []string{"func", "main", "fmt.Println", "return", "nil", "err", "if", "for", "select"}
	for _, tok := range sampleTokens {
		dict.GetOrRegister(tok)
	}

	// Train default sequence: "func" -> "main" -> "fmt.Println" -> "return" -> "nil",
	// labeling each association so traces can name the demo step that produced a prediction.
	mem.SetVocabSeed(dict.Seed())
	ctxLabel := "func"
	prevHV := dict.GetOrRegister("func")
	for _, tok := range []string{"main", "fmt.Println", "return", "nil"} {
		nextHV := dict.GetOrRegister(tok)
		mem.StoreLabeled(prevHV, nextHV, fmt.Sprintf("demo[%s]→%s", ctxLabel, tok))
		prevHV = prevHV.Permute(1).Bind(nextHV)
		ctxLabel += " " + tok
	}

	return &Server{
		Port:       port,
		Dict:       dict,
		Memory:     mem,
		Decoder:    dec,
		Router:     router,
		ASTIndexer: indexer,
		IndexRoot:  ".",
	}
}

// Start launches the HTTP and WebSocket server listeners.
func (s *Server) Start() error {
	http.HandleFunc("/ws", s.handleWebSocket)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "NeuroVSA HDC Engine operational. Dict Size: %d", s.Dict.Size())
	})

	addr := fmt.Sprintf(":%d", s.Port)
	log.Printf("[NeuroVSA] API Server listening on ws://localhost%s/ws", addr)
	return http.ListenAndServe(addr, nil)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("[NeuroVSA] Client connected from %s", conn.RemoteAddr())

	// Per-connection agent trajectory state, bound to the shared read-only router. Isolating
	// it here means concurrent clients never corrupt each other's routing history.
	traj := engine.NewTrajectoryTracker(s.Router)

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[NeuroVSA] Client disconnected: %v", err)
			break
		}

		var req ClientMessage
		if err := json.Unmarshal(msgBytes, &req); err != nil {
			s.sendError(conn, "Invalid JSON payload")
			continue
		}

		switch req.Type {
		case "prompt":
			s.handlePrompt(conn, req.Text)
		case "index_ast":
			s.handleIndexAST(conn, req.Path)
		case "route_tool":
			s.handleRouteTool(conn, traj, req.Goal)
		default:
			s.sendError(conn, "Unknown message type: "+req.Type)
		}
	}
}

func (s *Server) handlePrompt(conn *websocket.Conn, text string) {
	words := strings.Fields(text)
	if len(words) == 0 {
		s.sendDone(conn)
		return
	}

	// Encode the seed with the same permute-bind recurrence used during training, so a
	// multi-word seed aligns with the stored contexts.
	contextHV := engine.EncodeContext(s.Dict, words)

	// Execute autoregressive sequence prediction loop
	tokens, dists := s.Decoder.GenerateSequence(contextHV, 8)

	for i, tok := range tokens {
		time.Sleep(20 * time.Millisecond) // Stream delay for retro terminal visual effect
		s.sendJSON(conn, ServerResponse{
			Type:  "token",
			Value: tok,
			Dist:  dists[i],
		})
	}

	s.sendDone(conn)
}

func (s *Server) handleIndexAST(conn *websocket.Conn, targetPath string) {
	safePath, err := s.resolveIndexPath(targetPath)
	if err != nil {
		s.sendError(conn, fmt.Sprintf("AST indexing rejected: %v", err))
		return
	}

	fileMap, funcVecs, err := s.ASTIndexer.IndexDirectory(safePath)
	if err != nil {
		s.sendError(conn, fmt.Sprintf("AST Indexing error: %v", err))
		return
	}

	summary := fmt.Sprintf("Indexed %d Go files and %d function ASTs. Total tokens in dictionary: %d",
		len(fileMap), len(funcVecs), s.Dict.Size())

	s.sendJSON(conn, ServerResponse{
		Type:    "ast_done",
		Summary: summary,
	})
}

// resolveIndexPath resolves a client-supplied path against s.IndexRoot and refuses anything
// that escapes it. Absolute paths and parent-directory traversal ("..") are rejected, so a
// connected client can only index within the configured root — never arbitrary host directories.
func (s *Server) resolveIndexPath(requested string) (string, error) {
	if requested == "" {
		requested = "."
	}
	if filepath.IsAbs(requested) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	root := s.IndexRoot
	if root == "" {
		root = "."
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(rootAbs, requested)
	rel, err := filepath.Rel(rootAbs, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the index root")
	}
	return joined, nil
}

func (s *Server) handleRouteTool(conn *websocket.Conn, traj *engine.TrajectoryTracker, goal string) {
	if goal == "" {
		goal = "fix_bug"
	}

	// Starting a new goal resets the trajectory; repeating the same goal advances along it,
	// so successive /route calls walk the learned workflow (ASTSearch -> ReadFile -> ...).
	if traj.Goal() != goal {
		traj.SetGoal(goal)
	}

	tool, elapsed := traj.SelectNextTool()
	traj.RecordAction(tool)

	s.sendJSON(conn, ServerResponse{
		Type:    "trajectory",
		Action:  tool,
		Latency: elapsed.Microseconds(),
		Summary: traj.GetTrajectorySummary(),
	})
}

func (s *Server) sendJSON(conn *websocket.Conn, resp ServerResponse) {
	bytes, _ := json.Marshal(resp)
	conn.WriteMessage(websocket.TextMessage, bytes)
}

func (s *Server) sendDone(conn *websocket.Conn) {
	s.sendJSON(conn, ServerResponse{Type: "done"})
}

func (s *Server) sendError(conn *websocket.Conn, errMsg string) {
	s.sendJSON(conn, ServerResponse{Type: "error", Error: errMsg})
}
