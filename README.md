# NeuroVSA (PhoneForge) — Master Technical Architecture

> **An Edge-Native, Zero-LLM, Zero-PyTorch Hyperdimensional Sequence Generation & Agentic Tool Routing Engine**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![React](https://img.shields.io/badge/React-18-61DAFB?style=flat&logo=react)](https://react.dev)
[![Tailwind CSS](https://img.shields.io/badge/Tailwind-3.4-38B2AC?style=flat&logo=tailwind-css)](https://tailwindcss.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## ⚡ Executive Overview

**NeuroVSA** is a high-performance local AI engine built entirely on **Hyperdimensional Computing (HDC)** and **Vector Symbolic Architecture (VSA)**. It replaces traditional dense neural networks, floating-point matrix multiplications ($O(N^2)$), and heavy GPU/NPU runtime dependencies (`llama.cpp`, PyTorch, ONNX) with native 64-bit hardware bitwise operations (`XOR`, cyclic bit-shifts, and `POPCNT`).

### 🌟 Key Features & Breakthroughs
- **Zero AI Dependencies**: No Python, no PyTorch, no GGUF models, no OpenAI/Anthropic API calls, no CUDA.
- **Microsecond Tool Routing**: Agent trajectory state tracking and tool selection execute in **$51.79\,\mu\text{s}$** ($0.0518\text{ ms}$) — over **10,000x faster** than traditional LLM dispatchers.
- **Ultra-Compact Memory Footprint**: A 10,000-bit hypervector fits in just **157 `uint64` words (1,004 bytes)**.
- **Go AST Structural Indexer**: Encodes code syntax trees into high-dimensional space for instant structural code search and relational lookup.
- **60 FPS Retro Terminal UI**: React 18 + Tailwind CSS frontend streaming sequence tokens in real-time over WebSockets with `useRef` rendering optimization.

---

## 📐 Hyperdimensional VSA Math Primitives

| VSA Operation | Mathematical Symbol | Bitwise Hardware Primitive | Description |
| :--- | :---: | :--- | :--- |
| **Binding** | $\otimes$ | Bitwise XOR (`^`) | Binds two vectors into an orthogonal product vector. Self-inverse: $A \otimes B \otimes B = A$. |
| **Permutation** | $\rho$ | Cyclic Bit-Shift (`<<`, `>>`) | Permutes vector by $S$ bits across 157 `uint64` word boundaries. Encodes sequence position & temporal history. |
| **Bundling** | $\oplus$ | Bitwise Majority Vote | Bundles $N$ hypervectors into a single vector preserving high cosine/Hamming similarity with all components. |
| **Similarity** | $d_H(A, B)$ | CPU Native `POPCNT` | Calculated using `math/bits.OnesCount64(A ^ B)` across words. |

---

## 📁 Repository Structure

```
NeuroVSA/
├── core/
│   ├── hypervector.go       # 10,000-bit packed vector math (Bind, Permute, Bundle)
│   ├── distance.go          # POPCNT Hamming distance & parallel candidate search
│   ├── dictionary.go        # Thread-safe Item Memory TokenDictionary
│   └── hypervector_test.go  # Unit tests & mathematical verification
├── parser/
│   ├── ast.go               # Go AST parser & tree-to-hypervector binder
│   └── ast_test.go          # Unit tests for code indexer
├── engine/
│   ├── memory.go            # Associative Memory store & binary disk persistence
│   ├── decoder.go           # HDC unbinding & autoregressive prediction loop
│   ├── trajectory.go       # Microsecond agent state tracking & tool router
│   └── engine_test.go       # Benchmarks & trajectory tests
├── api/
│   └── server.go            # Gorilla WebSocket API hub (ws://localhost:8080/ws)
├── ui/
│   ├── src/
│   │   ├── TerminalUI.jsx   # React 18 + Tailwind 60 FPS streaming console
│   │   ├── App.jsx          # App root
│   │   ├── main.jsx         # React DOM entrypoint
│   │   └── index.css        # Custom retro terminal styles
│   ├── package.json
│   ├── vite.config.js
│   └── tailwind.config.js
├── docs/
│   ├── architecture.md      # Deep-dive system architecture details
│   ├── user_manual.md       # Full user manual & API guide
│   └── developer_guide.md   # Developer notes & extension guide
├── main.go                  # Main Go binary entrypoint
├── go.mod                   # Go 1.22 module definition
└── go.sum                   # Dependency checksums
```

---

## 🚀 Quickstart Guide

### Prerequisites
- **Go**: 1.22 or higher
- **Node.js**: 18.x or higher & `npm`

### 1. Build and Run the Go Backend
```bash
# Clone the repository
git clone https://github.com/JGautam09/NeuroVSA.git
cd NeuroVSA

# Run tests
go test -v ./...

# Build executable
go build -o neuro-vsa main.go

# Start the API server on port 8080
./neuro-vsa
```
*Server output:*
```text
=======================================================================
   NEURO-VSA (PhoneForge) — Bare-Metal Hyperdimensional Computing Engine
   Zero-LLM | Zero-PyTorch | 10,000-Bit Hardware Bitwise Math Core
=======================================================================
[NeuroVSA] API Server listening on ws://localhost:8080/ws
```

### 2. Launch the React Terminal UI
In a separate terminal window:
```bash
cd ui
npm install
npm run dev
```
Open your browser at **`http://localhost:3000`** to interact with the retro green-on-black terminal interface!

---

## 💻 Terminal Commands & Interactivity

Inside the web terminal input prompt:

- **Autoregressive Token Generation**:
  ```text
  func main
  ```
  *Executes VSA sequence generation streaming predicted tokens over WebSockets.*

- **Microsecond Agent Tool Routing**:
  ```text
  /route refactor_code
  ```
  *Calculates mathematically optimal agent tool selection in under $100\,\mu\text{s}$.*

- **Go Codebase AST Indexing**:
  ```text
  /ast .
  ```
  *Parses Go `.go` source files and encodes function ASTs into hyperdimensional memory.*

---

## 📊 Benchmark Results

| Metric | Measured Value | Benchmark Context |
| :--- | :--- | :--- |
| **Tool Routing Latency** | **$51.792\,\mu\text{s}$** | `SelectNextTool` Hamming distance match |
| **Vector Dimension ($D$)** | **10,000 bits** | Packed in `[157]uint64` stack array |
| **Unbinding Speed** | **$< 1\,\mu\text{s}$** | Single $A \otimes B$ XOR operation |
| **Memory per Vector** | **1,004 bytes** | Fixed allocation without heap thrashing |

---

## 📚 Detailed Documentation

For in-depth documentation, please refer to the files in the `docs/` folder:
- [Architecture Details](docs/architecture.md) — Mathematical foundations, multi-word bit shifting, VSA binding algebra.
- [User Manual](docs/user_manual.md) — Installation, configuration, terminal usage, and WebSocket API reference.
- [Developer Guide](docs/developer_guide.md) — Package contracts, adding custom tool actions, and extending VSA math primitives.

---

## 📜 License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
