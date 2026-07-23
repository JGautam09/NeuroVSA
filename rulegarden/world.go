package rulegarden

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/JGautam09/NeuroVSA/core"
	"github.com/JGautam09/NeuroVSA/engine"
)

// GridSize is the world's square dimension (locked MVP scope).
const GridSize = 24

// nearRadius is the Chebyshev distance at or under which a percept reads "near".
const nearRadius = 4

// Object is a non-learning world entity (food, predator, ...). Predators take one random
// step per tick; everything else is static.
type Object struct {
	ID    int  `json:"id"`
	Kind  Kind `json:"kind"`
	X     int  `json:"x"`
	Y     int  `json:"y"`
	Alive bool `json:"alive"`
}

// Creature is a teachable agent: position plus a glass-box brain and its latest decision.
type Creature struct {
	ID           int       `json:"id"`
	X            int       `json:"x"`
	Y            int       `json:"y"`
	Brain        *Brain    `json:"-"`
	LastDecision *Decision `json:"last_decision,omitempty"`
}

// Event is one entry of the world's event log — the replayable source of truth. Percepts and
// actions are symbolic (strings), never vectors, so packs stay tiny and human-readable.
type Event struct {
	Tick     int                   `json:"tick"`
	Op       string                `json:"op"` // spawn_creature | spawn_object | teach | transfer | forget
	Kind     Kind                  `json:"kind,omitempty"`
	X        int                   `json:"x,omitempty"`
	Y        int                   `json:"y,omitempty"`
	Creature int                   `json:"creature,omitempty"`
	Percept  *PerceptSpec          `json:"percept,omitempty"`
	Action   string                `json:"action,omitempty"`
	Lesson   *engine.AssociationID `json:"lesson,omitempty"` // "site:seq" in JSON
	NewSees  string                `json:"new_sees,omitempty"`
}

// World is fully defined by (Seed, Events): replaying the log against the seed reproduces a
// bit-identical world on any platform (see Hash and the replay golden test).
type World struct {
	Seed      uint64      `json:"seed"`
	Tick      int         `json:"tick"`
	Vocab     *Vocab      `json:"-"`
	Objects   []*Object   `json:"objects"`
	Creatures []*Creature `json:"creatures"`
	Events    []Event     `json:"events"`
	rng       *rng
	nextObjID int
	nextCrID  int
}

// NewWorld creates an empty world. The seed drives ONLY the PRNG; vocabulary vectors come
// from the fixed VocabSeed (see vocab.go) so worlds share one vocabulary.
func NewWorld(seed uint64) *World {
	return &World{
		Seed:  seed,
		Vocab: NewVocab(),
		rng:   newRNG(seed),
	}
}

// ---- event application (the ONLY mutation path besides Step) ----

// Apply validates an event, applies it, and appends it to the log at the current tick.
func (w *World) Apply(e Event) error {
	e.Tick = w.Tick
	switch e.Op {
	case "spawn_creature":
		if err := w.checkPos(e.X, e.Y); err != nil {
			return err
		}
		w.nextCrID++
		w.Creatures = append(w.Creatures, &Creature{ID: w.nextCrID, X: e.X, Y: e.Y, Brain: NewBrain(w.Vocab)})
	case "spawn_object":
		if err := w.checkPos(e.X, e.Y); err != nil {
			return err
		}
		valid := false
		for _, k := range Kinds {
			if k == e.Kind {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("unknown object kind %q", e.Kind)
		}
		w.nextObjID++
		w.Objects = append(w.Objects, &Object{ID: w.nextObjID, Kind: e.Kind, X: e.X, Y: e.Y, Alive: true})
	case "teach":
		c, err := w.creature(e.Creature)
		if err != nil {
			return err
		}
		if e.Percept == nil {
			return fmt.Errorf("teach event missing percept")
		}
		if _, err := c.Brain.Teach(*e.Percept, e.Action); err != nil {
			return err
		}
	case "transfer":
		c, err := w.creature(e.Creature)
		if err != nil {
			return err
		}
		if e.Lesson == nil {
			return fmt.Errorf("transfer event missing lesson id")
		}
		if _, err := c.Brain.Transfer(*e.Lesson, e.NewSees); err != nil {
			return err
		}
	case "forget":
		c, err := w.creature(e.Creature)
		if err != nil {
			return err
		}
		if e.Lesson == nil {
			return fmt.Errorf("forget event missing lesson id")
		}
		if err := c.Brain.Forget(*e.Lesson); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown event op %q", e.Op)
	}
	w.Events = append(w.Events, e)
	return nil
}

// ---- the tick ----

// Step advances the world one tick: creatures perceive → decide → act (in ID order), then
// predators take one random step. All randomness comes from the world PRNG in a fixed order,
// which is what makes replay exact.
func (w *World) Step() {
	for _, c := range w.Creatures {
		p, target := w.perceive(c)
		d := c.Brain.Decide(p)
		c.LastDecision = &d
		w.act(c, d.Action, target)
	}
	for _, o := range w.Objects {
		if o.Alive && o.Kind == KindPredator {
			dir := w.rng.intn(4)
			o.X, o.Y = clamp(o.X+dx[dir]), clamp(o.Y+dy[dir])
		}
	}
	w.Tick++
}

var dx = [4]int{0, 0, 1, -1} // N, S, E, W (N = -y)
var dy = [4]int{-1, 1, 0, 0}

// perceive returns the creature's symbolic percept (nearest living object) and the target.
func (w *World) perceive(c *Creature) (PerceptSpec, *Object) {
	var nearest *Object
	best := 1 << 30
	for _, o := range w.Objects {
		if !o.Alive {
			continue
		}
		d := cheb(c.X-o.X, c.Y-o.Y)
		if d < best || (d == best && nearest != nil && o.ID < nearest.ID) {
			best, nearest = d, o
		}
	}
	if nearest == nil {
		return PerceptSpec{Sees: SeesNothing}, nil
	}
	p := PerceptSpec{Sees: string(nearest.Kind), Dist: DistFar, Dir: bucketDir(nearest.X-c.X, nearest.Y-c.Y)}
	if best <= nearRadius {
		p.Dist = DistNear
	}
	return p, nearest
}

// act applies a decided action. move-toward/away step one cell relative to the percept
// target; eat consumes an adjacent food object; wander takes one PRNG step.
func (w *World) act(c *Creature, action string, target *Object) {
	switch action {
	case ActMoveToward:
		if target != nil {
			c.X, c.Y = clamp(c.X+sign(target.X-c.X)), clamp(c.Y+sign(target.Y-c.Y))
		}
	case ActMoveAway:
		if target != nil {
			c.X, c.Y = clamp(c.X-sign(target.X-c.X)), clamp(c.Y-sign(target.Y-c.Y))
		}
	case ActEat:
		if target != nil && target.Kind == KindFood && cheb(c.X-target.X, c.Y-target.Y) <= 1 {
			target.Alive = false
		}
	case ActWander:
		dir := w.rng.intn(4)
		c.X, c.Y = clamp(c.X+dx[dir]), clamp(c.Y+dy[dir])
	}
}

// ---- pack (share/replay) ----

// Pack is the shareable form of a world: seed + event log (+ how far it has ticked).
type Pack struct {
	Version int     `json:"version"`
	Seed    uint64  `json:"seed"`
	Ticks   int     `json:"ticks"`
	Events  []Event `json:"events"`
}

// Export captures the world as a replayable pack.
func (w *World) Export() Pack {
	events := make([]Event, len(w.Events))
	copy(events, w.Events)
	return Pack{Version: 1, Seed: w.Seed, Ticks: w.Tick, Events: events}
}

// Replay reconstructs a world from a pack by re-running its event log against its seed:
// events apply at their recorded ticks, interleaved with Steps, ending at pack.Ticks.
func Replay(p Pack) (*World, error) {
	if p.Version != 1 {
		return nil, fmt.Errorf("unsupported pack version %d", p.Version)
	}
	w := NewWorld(p.Seed)
	i := 0
	for w.Tick <= p.Ticks {
		for i < len(p.Events) && p.Events[i].Tick == w.Tick {
			if err := w.Apply(p.Events[i]); err != nil {
				return nil, fmt.Errorf("replaying event %d: %w", i, err)
			}
			i++
		}
		if w.Tick == p.Ticks {
			break
		}
		w.Step()
	}
	if i != len(p.Events) {
		return nil, fmt.Errorf("pack has %d events beyond its tick horizon", len(p.Events)-i)
	}
	return w, nil
}

// ExportJSON / ImportJSON are the wire forms used by the UI and share links.
func (w *World) ExportJSON() ([]byte, error) {
	return json.Marshal(w.Export())
}

func ImportJSON(data []byte) (*World, error) {
	var p Pack
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("invalid pack: %w", err)
	}
	return Replay(p)
}

// ---- determinism hash ----

// Hash returns a SHA-256 over the world's complete observable state: tick, PRNG state,
// objects, and every creature's position, brain matrix, and ledger. Two worlds with equal
// hashes are behaviorally identical from this point on — the replay guarantee is
// "same pack ⇒ same hash", enforced by a golden test.
func (w *World) Hash() string {
	h := sha256.New()
	le := binary.LittleEndian

	var buf [8]byte
	le.PutUint64(buf[:], uint64(w.Tick))
	h.Write(buf[:])
	le.PutUint64(buf[:], w.rng.state)
	h.Write(buf[:])

	objs := make([]*Object, len(w.Objects))
	copy(objs, w.Objects)
	sort.Slice(objs, func(a, b int) bool { return objs[a].ID < objs[b].ID })
	for _, o := range objs {
		fmt.Fprintf(h, "O|%d|%s|%d|%d|%v\n", o.ID, o.Kind, o.X, o.Y, o.Alive)
	}

	crs := make([]*Creature, len(w.Creatures))
	copy(crs, w.Creatures)
	sort.Slice(crs, func(a, b int) bool { return crs[a].ID < crs[b].ID })
	for _, c := range crs {
		fmt.Fprintf(h, "C|%d|%d|%d\n", c.ID, c.X, c.Y)
		m := c.Brain.Memory().Matrix()
		for i := 0; i < core.NumWords; i++ {
			le.PutUint64(buf[:], m.Vector[i])
			h.Write(buf[:])
		}
		for _, rec := range c.Brain.Lessons() {
			fmt.Fprintf(h, "L|%s|%s|%v\n", rec.ID, rec.Label, rec.Removed)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ---- helpers ----

func (w *World) creature(id int) (*Creature, error) {
	for _, c := range w.Creatures {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, fmt.Errorf("unknown creature %d", id)
}

func (w *World) checkPos(x, y int) error {
	if x < 0 || y < 0 || x >= GridSize || y >= GridSize {
		return fmt.Errorf("position (%d,%d) outside the %dx%d grid", x, y, GridSize, GridSize)
	}
	return nil
}

func cheb(dx, dy int) int {
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

// bucketDir maps a delta to N/S/E/W by dominant axis (E/W wins ties — deterministic).
func bucketDir(dx, dy int) string {
	ax, ay := dx, dy
	if ax < 0 {
		ax = -ax
	}
	if ay < 0 {
		ay = -ay
	}
	if ax >= ay {
		if dx >= 0 {
			return "E"
		}
		return "W"
	}
	if dy >= 0 {
		return "S"
	}
	return "N"
}

func sign(v int) int {
	if v > 0 {
		return 1
	}
	if v < 0 {
		return -1
	}
	return 0
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v >= GridSize {
		return GridSize - 1
	}
	return v
}
