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

	// Decision-time snapshot for receipts: the brain image AS IT WAS when LastDecision was
	// made, so a certificate reflects the historical decision even after later teaching or
	// forgetting. Captured lazily in Step (only when the brain's epoch changed), unexported so
	// it is neither serialized nor part of the world hash.
	snapImage []byte
	snapEpoch uint64
	hasSnap   bool
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
	// ForeignPack embeds another player's full world pack for merge_brains events, so a
	// merged world remains completely defined by its own seed + event log (worlds quote the
	// worlds they learned from — replay stays bit-exact).
	ForeignPack *Pack `json:"foreign_pack,omitempty"`
	// LessonPack embeds a FLAT engine lesson pack for apply_pack / revoke_pack events — the
	// live-sync vehicle. Unlike ForeignPack it contains no world events, so a sync stream
	// never grows merge nesting; applying is an idempotent NeuroMesh merge and revoking
	// creates tombstones deterministically at replay time.
	LessonPack *engine.Pack `json:"lesson_pack,omitempty"`
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

// Replay/merge safety bounds. A world pack is untrusted input (it can arrive pasted from
// another player), so its size, tick horizon, event count, and merge-nesting depth are all
// capped before any synchronous replay runs.
const (
	MaxPackBytes    = 8 << 20 // 8 MiB of pack JSON
	MaxReplayTicks  = 20000   // tick horizon a single pack may drive
	MaxReplayEvents = 20000   // events a single pack may carry
	MaxMergeDepth   = 4       // how deep merge_brains packs may nest
)

// Apply validates an event, applies it, and appends it to the log at the current tick.
func (w *World) Apply(e Event) error {
	return w.applyDepth(e, 0)
}

// applyDepth is Apply with a merge-nesting depth, so recursively replayed foreign packs
// cannot exceed MaxMergeDepth.
func (w *World) applyDepth(e Event, depth int) error {
	e.Tick = w.Tick
	switch e.Op {
	case "spawn_creature":
		if err := w.checkPos(e.X, e.Y); err != nil {
			return err
		}
		w.nextCrID++
		brain := NewBrain(w.Vocab)
		// Each brain claims a deterministic writer site derived from (world seed, creature
		// id): stable under replay, distinct across worlds — the NeuroMesh precondition for
		// merging brains between players' worlds.
		brain.Memory().SetSite(creatureSite(w.Seed, w.nextCrID))
		w.Creatures = append(w.Creatures, &Creature{ID: w.nextCrID, X: e.X, Y: e.Y, Brain: brain})
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
	case "merge_brains":
		c, err := w.creature(e.Creature)
		if err != nil {
			return err
		}
		if e.ForeignPack == nil {
			return fmt.Errorf("merge_brains event missing foreign pack")
		}
		if err := w.mergeBrains(c, *e.ForeignPack, depth); err != nil {
			return err
		}
	case "apply_pack":
		c, err := w.creature(e.Creature)
		if err != nil {
			return err
		}
		if err := validLessonPack(e.LessonPack); err != nil {
			return err
		}
		if err := c.applyLessonPack(func(scratch *engine.AssociativeMemory) error {
			_, err := engine.ApplyPack(scratch, e.LessonPack)
			return err
		}); err != nil {
			return fmt.Errorf("apply_pack: %w", err)
		}
	case "revoke_pack":
		c, err := w.creature(e.Creature)
		if err != nil {
			return err
		}
		if err := validLessonPack(e.LessonPack); err != nil {
			return err
		}
		if err := c.applyLessonPack(func(scratch *engine.AssociativeMemory) error {
			engine.RevokePack(scratch, e.LessonPack) // 0 removals is fine — revoking is idempotent
			return nil
		}); err != nil {
			return fmt.Errorf("revoke_pack: %w", err)
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
		c.snapshotBrain() // capture the memory image matching THIS decision (for receipts)
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
// PublicKey/Signature optionally bind the pack to its author's ed25519 key over
// CanonicalBytes (see sign.go); replay refuses a signed pack whose signature no longer
// matches its content, while unsigned packs replay as before.
type Pack struct {
	Version   int     `json:"version"`
	Seed      uint64  `json:"seed"`
	Ticks     int     `json:"ticks"`
	Events    []Event `json:"events"`
	PublicKey []byte  `json:"public_key,omitempty"`
	Signature []byte  `json:"signature,omitempty"`
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
	return replayDepth(p, 0)
}

// replayDepth is Replay with the current merge-nesting depth. It enforces the tick, event,
// and depth bounds before running the synchronous replay loop, so an untrusted pack cannot
// freeze the caller.
func replayDepth(p Pack, depth int) (*World, error) {
	if p.Version != 1 {
		return nil, fmt.Errorf("unsupported pack version %d", p.Version)
	}
	// Tamper evidence: a signed pack whose content no longer matches its signature is
	// refused outright. Runs at every nesting level, so a foreign pack embedded in a
	// merge_brains event is re-verified whenever the quoting world replays.
	if len(p.Signature) > 0 && !p.VerifySignature() {
		return nil, fmt.Errorf("pack signature is invalid — content does not match its author's signature")
	}
	if depth > MaxMergeDepth {
		return nil, fmt.Errorf("merge nesting exceeds the limit of %d", MaxMergeDepth)
	}
	if p.Ticks < 0 || p.Ticks > MaxReplayTicks {
		return nil, fmt.Errorf("pack tick horizon %d outside [0, %d]", p.Ticks, MaxReplayTicks)
	}
	if len(p.Events) > MaxReplayEvents {
		return nil, fmt.Errorf("pack carries %d events, exceeding the limit of %d", len(p.Events), MaxReplayEvents)
	}
	w := NewWorld(p.Seed)
	i := 0
	for w.Tick <= p.Ticks {
		for i < len(p.Events) && p.Events[i].Tick == w.Tick {
			if err := w.applyDepth(p.Events[i], depth); err != nil {
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
	if len(data) > MaxPackBytes {
		return nil, fmt.Errorf("pack is %d bytes, exceeding the %d-byte limit", len(data), MaxPackBytes)
	}
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

// ---- merge worlds (NeuroMesh in costume) ----

// mergeBrains replays the foreign world pack and merges EVERY creature brain it contains
// into the target creature's brain. Atomic: the merges run against a clone of the target
// memory, which replaces the live brain only if every source merges cleanly — a failed merge
// (e.g. a same-seed site collision) leaves the world untouched, preserving replay integrity.
func (w *World) mergeBrains(c *Creature, foreign Pack, depth int) error {
	if depth+1 > MaxMergeDepth {
		return fmt.Errorf("merge nesting exceeds the limit of %d", MaxMergeDepth)
	}
	fw, err := replayDepth(foreign, depth+1)
	if err != nil {
		return fmt.Errorf("replaying foreign pack: %w", err)
	}
	if len(fw.Creatures) == 0 {
		return fmt.Errorf("foreign pack contains no creatures")
	}

	img, err := c.Brain.Memory().MarshalBinary()
	if err != nil {
		return err
	}
	scratch := engine.NewAssociativeMemory()
	if err := scratch.UnmarshalBinary(img); err != nil {
		return err
	}
	for _, fc := range fw.Creatures {
		if _, err := scratch.Merge(fc.Brain.Memory()); err != nil {
			return fmt.Errorf("merging brain of foreign creature %d: %w", fc.ID, err)
		}
	}
	c.Brain.replaceMemory(scratch)
	return nil
}

// MergeBrainsFrom is the public API behind merge_brains: it applies (and logs) the event.
func (w *World) MergeBrainsFrom(foreign Pack, creatureID int) error {
	return w.Apply(Event{Op: "merge_brains", Creature: creatureID, ForeignPack: &foreign})
}

// ---- live sync (flat lesson-pack events) ----

// MaxLessonPackEntries bounds a single apply_pack/revoke_pack event. A whole healthy brain
// is ~RecommendedMaxActiveAssociations lessons, so this cap is generous while keeping a
// hostile pack from bloating the event log.
const MaxLessonPackEntries = 1024

// validLessonPack is the untrusted-input gate for sync events: present, bounded, and — the
// same policy as signed world packs — a SIGNED pack whose signature does not verify is
// refused outright (unsigned packs are allowed and surfaced as such by the UI layer).
func validLessonPack(p *engine.Pack) error {
	if p == nil {
		return fmt.Errorf("event missing lesson pack")
	}
	if len(p.Entries) > MaxLessonPackEntries {
		return fmt.Errorf("lesson pack carries %d entries, exceeding the limit of %d", len(p.Entries), MaxLessonPackEntries)
	}
	if len(p.Signature) > 0 && !p.VerifySignature() {
		return fmt.Errorf("lesson pack signature is invalid — content does not match its author's signature")
	}
	return nil
}

// applyLessonPack runs op against a scratch clone of the creature's brain memory and commits
// atomically — a failing op leaves the brain untouched (the same discipline as mergeBrains).
func (c *Creature) applyLessonPack(op func(*engine.AssociativeMemory) error) error {
	img, err := c.Brain.Memory().MarshalBinary()
	if err != nil {
		return err
	}
	scratch := engine.NewAssociativeMemory()
	if err := scratch.UnmarshalBinary(img); err != nil {
		return err
	}
	if err := op(scratch); err != nil {
		return err
	}
	c.Brain.replaceMemory(scratch)
	return nil
}

// ApplyLessonPackTo installs a flat lesson pack into a creature's brain as a logged,
// replayable apply_pack event — the receive half of live sync. Idempotent: re-applying the
// same pack is a no-op merge.
func (w *World) ApplyLessonPackTo(p engine.Pack, creatureID int) error {
	return w.Apply(Event{Op: "apply_pack", Creature: creatureID, LessonPack: &p})
}

// RevokeLessonPackFrom tombstones a pack's entries in a creature's brain as a logged,
// replayable revoke_pack event — how a peer's forget propagates during live sync.
func (w *World) RevokeLessonPackFrom(p engine.Pack, creatureID int) error {
	return w.Apply(Event{Op: "revoke_pack", Creature: creatureID, LessonPack: &p})
}

// BrainPacks exports a creature's ACTIVE lessons as flat engine packs, one per writer site
// — the send half of live sync (both the on-connect snapshot and the after-mutation
// broadcast; receivers dedupe via merge idempotence). One pack per site is what keeps every
// lesson under its ORIGINAL author identity: a brain that already absorbed a friend's
// lessons relays them with the friend's site intact, never re-attributed.
func (w *World) BrainPacks(name string, creatureID int) ([]engine.Pack, error) {
	c, err := w.creature(creatureID)
	if err != nil {
		return nil, err
	}
	return engine.PacksFromMemory(name, c.Brain.Memory()), nil
}

// RevocationPack builds the pack a peer needs to tombstone one forgotten lesson. The bound
// vector is intentionally zero — revocation is by (site, seq) identity, not content — so a
// revocation pack can never be replayed as an apply_pack to re-teach the lesson.
func (w *World) RevocationPack(name string, creatureID int, lesson engine.AssociationID) (engine.Pack, error) {
	c, err := w.creature(creatureID)
	if err != nil {
		return engine.Pack{}, err
	}
	for _, rec := range c.Brain.Lessons() {
		if rec.ID == lesson {
			return engine.Pack{
				Name:      name,
				VocabSeed: c.Brain.Memory().VocabSeed(),
				Site:      lesson.Site,
				Entries:   []engine.PackEntry{{Seq: lesson.Seq, Label: rec.Label}},
			}, nil
		}
	}
	return engine.Pack{}, fmt.Errorf("creature %d has no lesson %s", creatureID, lesson)
}

// ---- receipts (ProofRoute in costume) ----

// snapshotBrain lazily captures the brain image whenever the brain has mutated since the last
// snapshot. Called right after a decision in Step, so snapImage always matches LastDecision.
func (c *Creature) snapshotBrain() {
	if c.hasSnap && c.snapEpoch == c.Brain.Epoch() {
		return
	}
	img, err := c.Brain.Memory().MarshalBinary()
	if err != nil {
		return // in-memory marshal cannot fail; guard defensively
	}
	c.snapImage = img
	c.snapEpoch = c.Brain.Epoch()
	c.hasSnap = true
}

// instinctActionToken is the certificate-space token for the instinct default action.
var instinctActionToken = "action:" + ActWander

// CertifyCreature issues a replay-verifiable DecisionCertificate for the creature's LAST
// decision, made against the memory image AS IT WAS at that decision (not the current
// memory), together with that image — the pair nvsa-verify re-executes. The certificate's
// ExecutedAction/Basis are re-derivable policy fields, so instinct overrides (raw winner ≠
// executed action) are certified correctly, not hidden in a note.
func (w *World) CertifyCreature(id int) (engine.DecisionCertificate, []byte, error) {
	c, err := w.creature(id)
	if err != nil {
		return engine.DecisionCertificate{}, nil, err
	}
	d := c.LastDecision
	if d == nil || !c.hasSnap {
		return engine.DecisionCertificate{}, nil, fmt.Errorf("creature %d has no decision yet — tick the world", id)
	}

	// Re-execute against the decision-time image so the fingerprint anchors to the memory
	// that actually made this decision.
	mem := engine.NewAssociativeMemory()
	if err := mem.UnmarshalBinary(c.snapImage); err != nil {
		return engine.DecisionCertificate{}, nil, err
	}
	tokens := make([]string, len(Actions))
	for i, a := range Actions {
		tokens[i] = "action:" + a
	}
	cert, err := engine.IssueDecision(mem, w.Vocab.EncodePercept(d.Percept), tokens)
	if err != nil {
		return engine.DecisionCertificate{}, nil, err
	}

	cert.MinMargin = MinDecisionMargin
	cert.InstinctAction = instinctActionToken
	cert.ExecutedAction, cert.Basis = engine.DerivePolicyOutcome(
		mem.Total(), cert.Candidates, len(cert.Contributors), MinDecisionMargin, instinctActionToken)

	// Guard: the certified executed action MUST equal what the creature actually did. Same
	// policy on both sides, so this only fires on a real bug — never ship a contradicting
	// receipt.
	if cert.ExecutedAction != "action:"+d.Action || cert.Basis != d.Basis {
		return engine.DecisionCertificate{}, nil, fmt.Errorf(
			"receipt/decision divergence for creature %d: certified %s/%s but acted %s/%s",
			id, cert.ExecutedAction, cert.Basis, "action:"+d.Action, d.Basis)
	}
	cert.Note = fmt.Sprintf("rulegarden world=%d creature=%d percept=%s", w.Seed, id, d.Percept)
	return cert, c.snapImage, nil
}

// creatureSite derives a brain's writer identity from (world seed, creature id) via a
// splitmix64 mix — stable under replay, distinct across world seeds. Two worlds sharing a
// seed produce the same sites; merging their divergent histories is refused by the engine's
// collision check (documented limitation: merge partners should use different seeds).
func creatureSite(worldSeed uint64, creatureID int) uint64 {
	r := newRNG(worldSeed ^ (0xC2B2AE3D27D4EB4F * uint64(creatureID)))
	return r.next()
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
