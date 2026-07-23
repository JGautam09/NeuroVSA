# NeuroVSA Master Architectural Specification

## 1. High-Dimensional Vector Geometry & VSA Math

Hyperdimensional Computing (HDC) operates on the principle that brains and high-capacity symbolic cognitive architectures represent concepts as massive pattern vectors across high-dimensional space ($D = 10,000$).

### 1.1 Quasi-Orthogonality
In a 10,000-dimensional binary space $\{0, 1\}^{10000}$, the total number of possible distinct states is $2^{10000}$. Any two randomly generated hypervectors $A$ and $B$ have an expected Hamming distance of:

$$\mathbb{E}[d_H(A, B)] = 5,000 \quad (50\% \text{ bit difference})$$

The probability density function drops off exponentially around 5,000 bits. Vectors with $d_H \approx 5,000$ are mathematically **quasi-orthogonal** (uncorrelated).

---

## 2. Bitwise Memory Packing Mechanics

To maximize execution efficiency on modern 64-bit CPUs, NeuroVSA packs 10,000 bits into a contiguous fixed stack array of 157 `uint64` words:

$$157 \times 64 = 10,048 \text{ bits}$$

```
[ Word 0 ]   Bits 0   .. 63
[ Word 1 ]   Bits 64  .. 127
...
[ Word 155 ] Bits 9920 .. 9983
[ Word 156 ] Bits 9984 .. 10047 (Bits 0..15 active, bits 16..63 masked to 0)
```

### Last Word Mask Constraint
To prevent unmasked garbage bits in word 156 from corrupting POPCNT Hamming distance computations or XOR operations:
```go
const LastWordMask uint64 = 0xFFFF // Keeps bits 0..15
hv.Vector[156] &= LastWordMask
```

---

## 3. Core VSA Math Primitives

### 3.1 Binding Operator ($\otimes$)
Binding is implemented using bitwise **XOR** (`^`):
$$C = A \otimes B \implies C_i = A_i \oplus_{\text{XOR}} B_i$$

**Properties**:
1. **Self-Inverse**: $A \otimes B \otimes B = A$ (unbinding is identical to binding).
2. **Orthogonality Preserving**: $C$ is quasi-orthogonal to both $A$ and $B$ ($d_H(C, A) \approx 5000$).
3. **Distance Preserving**: $d_H(A \otimes C, B \otimes C) = d_H(A, B)$.

### 3.2 Cyclic Permutation Operator ($\rho$)
Permutation shifts all 10,000 bits cyclicly left by $S$ positions across the 157 word boundaries:
$$\rho^S(V)_k = V_{(k - S) \bmod 10000}$$

**Properties**:
1. **Sequence Order Encoding**: $\rho^1(A) \otimes B \neq A \otimes \rho^1(B)$.
2. **Orthogonal to Base**: $d_H(\rho^S(A), A) \approx 5000$ for any $S \neq 0$.
3. **Invertible**: $\rho^{-S}(\rho^S(A)) = A$.

### 3.3 Bundling Operator ($\oplus$)
Bundling combines $N$ hypervectors into a single composite vector using **Bitwise Majority Vote**:
$$\left(\bigoplus_{j=1}^N V^{(j)}\right)_k = \begin{cases} 1 & \text{if } \sum_{j=1}^N V^{(j)}_k > \frac{N}{2} \\ 0 & \text{if } \sum_{j=1}^N V^{(j)}_k < \frac{N}{2} \\ \text{TieBreak}(k) & \text{if } \sum_{j=1}^N V^{(j)}_k = \frac{N}{2} \end{cases}$$

**Properties**:
1. **Similarity Preservation**: The bundled vector maintains a low Hamming distance ($d_H < 4000$) to all constituent vectors $V^{(j)}$.
2. **Noise Accumulation**: Bundling up to $N \approx 50\text{--}100$ vectors retains recognizable component identity upon unbinding.

---

## 4. AST Code Indexer Architecture

The parser transforms Go Abstract Syntax Trees (ASTs) into high-dimensional space:

```
Function Declaration: func Add(a int, b int) int
 ├── FuncName: "Add"       ──> V_func
 ├── Param 1:  "a"          ──> ρ^1(V_param_a)
 ├── Param 2:  "b"          ──> ρ^2(V_param_b)
 └── Return:   "int"        ──> ρ^3(V_ret_int)
```

**AST Hypervector Formula**:
$$V_{\text{AST}} = V_{\text{func}} \oplus \rho^1(V_{\text{param\_a}}) \oplus \rho^2(V_{\text{param\_b}}) \oplus \rho^3(V_{\text{ret\_int}})$$

---

## 5. Agent Trajectory Tracker & Microsecond Tool Router

Agent states are tracked continuously without any probabilistic LLM sampling:

$$\text{State}_{t+1} = \rho^1(\text{State}_t) \otimes V_{\text{action}_{t+1}}$$

### Learned Routing Policy

Routing is driven by a **policy associative memory** rather than a bare distance-to-goal
comparison. Registering a workflow (a goal plus its ordered tool sequence) stores a
`state → next-action` association at every step, exactly as an agent advances at runtime:

$$V_{\text{Policy}} = \bigoplus_{\text{workflows}} \; \bigoplus_{k} \big(\text{State}_k \otimes V_{\text{action}_{k+1}}\big), \qquad \text{State}_{k+1} = \rho^1(\text{State}_k) \otimes V_{\text{action}_{k+1}}$$

### Tool Selection Algorithm ($< 1\text{ ms}$)
1. Unbind the current trajectory state against the policy memory:
   $$V_{\text{query}} = V_{\text{Policy}} \otimes \text{State}_t$$
2. Clean up $V_{\text{query}}$ against the tool vocabulary (minimum Hamming distance):
   $$T^* = \arg\min_{T_j} \; d_H(V_{\text{query}}, V_{T_j})$$
3. Record $T^*$ to advance the trajectory, so successive selections walk the learned workflow.

Because the state encodes both the goal and the actions taken so far, selection is
goal- and history-dependent. Execution latency: **$\approx 1.03\,\mu\text{s}$** (Apple M5 Pro).
