package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"neuro-vsa/core"
	"neuro-vsa/engine"
	"neuro-vsa/parser"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all CORS connections for dev UI
	},
}

// Server encapsulates the NeuroVSA WebSocket API server.
type Server struct {
	mu          sync.Mutex
	Port        int
	Dict        *core.TokenDictionary
	Memory      *engine.AssociativeMemory
	Decoder     *engine.HDCDecoder
	Trajectory  *engine.TrajectoryTracker
	ASTIndexer  *parser.CodeASTIndexer
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
	Type     string `json:"type"`
	Value    string `json:"value,omitempty"`
	Dist     int    `json:"dist,omitempty"`
	Action   string `json:"action,omitempty"`
	Latency  int64  `json:"latency_us,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Error    string `json:"error,omitempty"`
}

// NewServer initializes a new API server instance.
func NewServer(port int) *Server {
	dict := core.NewTokenDictionary()
	mem := engine.NewAssociativeMemory()
	dec := engine.NewHDCDecoder(mem, dict)
	traj := engine.NewTrajectoryTracker()
	indexer := parser.NewCodeASTIndexer(dict)

	// Populate initial dictionary with sample sequences for demonstration
	sampleTokens := []string{"func", "main", "fmt.Println", "return", "nil", "err", "if", "for", "select"}
	for _, tok := range sampleTokens {
		dict.GetOrRegister(tok)
	}

	// Train default sequence: "func" -> "main" -> "fmt.Println" -> "return" -> "nil"
	prevHV := dict.GetOrRegister("func")
	for _, tok := range []string{"main", "fmt.Println", "return", "nil"} {
		nextHV := dict.GetOrRegister(tok)
		mem.StoreAssociation(prevHV, nextHV)
		prevHV = prevHV.Permute(1).Bind(nextHV)
	}

	return &Server{
		Port:       port,
		Dict:       dict,
		Memory:     mem,
		Decoder:    dec,
		Trajectory: traj,
		ASTIndexer: indexer,
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
			s.handleRouteTool(conn, req.Goal)
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

	// Tokenize input string and encode into context hypervector
	var components []core.Hypervector
	for i, word := range words {
		hv := s.Dict.GetOrRegister(word)
		components = append(components, hv.Permute(i))
	}
	contextHV := core.Bundle(components)

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
	if targetPath == "" {
		targetPath = "."
	}

	fileMap, funcVecs, err := s.ASTIndexer.IndexDirectory(targetPath)
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

func (s *Server) handleRouteTool(conn *websocket.Conn, goal string) {
	if goal == "" {
		goal = "default_goal"
	}

	s.Trajectory.SetGoal(goal)
	goalHV := s.Dict.GetOrRegister("goal:" + goal)

	tool, elapsed := s.Trajectory.SelectNextTool(goalHV)
	s.Trajectory.RecordAction(tool)

	s.sendJSON(conn, ServerResponse{
		Type:    "trajectory",
		Action:  tool,
		Latency: elapsed.Microseconds(),
		Summary: s.Trajectory.GetTrajectorySummary(),
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
