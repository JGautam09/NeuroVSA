# NeuroVSA User Manual & API Guide

> For what NeuroVSA is good at — and where a small embedding router beats it — see the
> [main README](../README.md#honest-limitations) and the benchmark in [`arena/`](../arena/).

## 1. System Requirements & Setup

### Prerequisites
- **Operating System**: macOS (ARM64/x86_64), Linux, or Windows.
- **Go**: Version 1.22 or newer.
- **Node.js**: Version 18.0 or newer.

---

## 2. Installation & Building

### Step 1: Clone and Test
```bash
git clone https://github.com/JGautam09/NeuroVSA.git
cd NeuroVSA

# Execute unit tests across all packages
go test -v ./...
```

### Step 2: Build Executable
```bash
go build -o neuro-vsa .
```

### Step 3: Setup UI Dependencies
```bash
cd ui
npm install
```

---

## 3. Running NeuroVSA

### Running the Go WebSocket Server
From the root directory:
```bash
./neuro-vsa -port 8080
```
*Options*:
- `-port <number>`: Port for WebSocket server (default: `8080`).
- `-index-root <dir>`: Directory the `/ast` indexer is confined to (default: `.`). Client-supplied paths cannot escape it.
- `-allow-all-origins`: Allow WebSocket connections from any origin. Off by default — only loopback origins are accepted, matching the local/air-gapped posture.

### Running the Web Terminal UI
From the `ui` directory:
```bash
npm run dev
```
Navigate to `http://localhost:3000` in your web browser.

---

## 4. Interactive Terminal Commands

Inside the retro terminal interface input box:

### 1. Token Sequence Prediction
Type any input prompt to trigger autoregressive sequence prediction:
```text
func main
```

### 2. Microsecond Tool Routing
Type `/route` followed by a goal. Seeded workflows include `fix_bug`, `add_feature`,
`refactor_code`, `deploy_service`, `write_docs`, and `call_api`; repeating the same goal walks
its learned tool sequence.
```text
/route fix_bug
```
*Output*:
```text
[AGENT ROUTER] Tool: ASTSearch | Latency: 1µs | Goal: "fix_bug", Step Count: 1, History: [ASTSearch]
```

### 3. Codebase AST Indexing
Type `/ast` followed by directory path:
```text
/ast .
```
*Output*:
```text
[AST INDEXER] Indexed 4 Go files and 18 function ASTs. Total tokens in dictionary: 42
```

---

## 5. WebSocket API Protocol Specification

Endpoint: `ws://localhost:8080/ws`

### Request Payloads (Client -> Server)

#### 1. Predict Sequence Prompt
```json
{
  "type": "prompt",
  "text": "func main"
}
```

#### 2. Agent Tool Routing Request
```json
{
  "type": "route_tool",
  "goal": "refactor_code"
}
```

#### 3. Index Codebase AST Request
```json
{
  "type": "index_ast",
  "path": "."
}
```

### Response Payloads (Server -> Client)

#### Streaming Token Response
```json
{
  "type": "token",
  "value": "fmt.Println",
  "dist": 42
}
```

#### Trajectory Routing Response
```json
{
  "type": "trajectory",
  "action": "ReadFile",
  "latency_us": 1,
  "summary": "Goal: \"fix_bug\", Step Count: 2, History: [ASTSearch, ReadFile]"
}
```

#### Completion Response
```json
{
  "type": "done"
}
```

---

## 6. Troubleshooting

- **WebSocket Connection Refused (`ws://localhost:8080/ws`)**:
  Ensure `./neuro-vsa` server is actively running in a terminal window.
- **Port Conflict**:
  If port 8080 is in use, start with `./neuro-vsa -port 8085` and update `TerminalUI.jsx`.
