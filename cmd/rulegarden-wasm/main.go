//go:build js && wasm

// Command rulegarden-wasm is the browser bridge for the RuleGarden world. It exposes a
// single global object `RuleGarden` whose methods take and return JSON strings — keeping the
// js.Value surface minimal and every payload inspectable in devtools. The entire engine runs
// inside the page: no server, no model file, no network calls.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"syscall/js"

	"github.com/JGautam09/NeuroVSA/core"
	"github.com/JGautam09/NeuroVSA/engine"
	"github.com/JGautam09/NeuroVSA/rulegarden"
)

var world *rulegarden.World

// identity is the player's ed25519 key, loaded by the page from IndexedDB (or freshly
// generated). While set, receipts and exported world packs are signed automatically —
// signing is additive: everything stays replay-verifiable exactly as before, the signature
// just also binds the author. The seed never leaves the page except through the explicit
// "export key" backup flow.
var identity ed25519.PrivateKey

// identityInfo is the uniform identity payload: the public half plus its display
// fingerprint (the trust-list handle; never a substitute for signature verification).
func identityInfo(priv ed25519.PrivateKey) map[string]any {
	pub := priv.Public().(ed25519.PublicKey)
	return map[string]any{
		"public_key_b64": base64.StdEncoding.EncodeToString(pub),
		"fingerprint":    engine.KeyFingerprint(pub),
	}
}

// packSignatureInfo summarizes a pasted pack's signature state for the UI: unsigned packs
// are allowed (surfaced as such), invalid ones never reach here — replay refuses them.
func packSignatureInfo(p rulegarden.Pack) map[string]any {
	if len(p.Signature) == 0 {
		return map[string]any{"signed": false}
	}
	return map[string]any{
		"signed":      true,
		"valid":       p.VerifySignature(),
		"fingerprint": p.SignerFingerprint(),
	}
}

// reply marshals a result or error into the uniform {ok, data|error} envelope.
func reply(data any, err error) string {
	type envelope struct {
		OK    bool   `json:"ok"`
		Data  any    `json:"data,omitempty"`
		Error string `json:"error,omitempty"`
	}
	var e envelope
	if err != nil {
		e = envelope{OK: false, Error: err.Error()}
	} else {
		e = envelope{OK: true, Data: data}
	}
	b, marshalErr := json.Marshal(e)
	if marshalErr != nil {
		return `{"ok":false,"error":"internal: reply marshal failed"}`
	}
	return string(b)
}

// state is the render payload: everything the canvas and panels need each frame.
func state() any {
	type creatureView struct {
		ID       int                  `json:"id"`
		X        int                  `json:"x"`
		Y        int                  `json:"y"`
		Decision *rulegarden.Decision `json:"decision,omitempty"`
		Lessons  []lessonView         `json:"lessons"`
	}
	type view struct {
		Seed      core.QuotedU64       `json:"seed"`
		Tick      int                  `json:"tick"`
		GridSize  int                  `json:"grid_size"`
		Objects   []*rulegarden.Object `json:"objects"`
		Creatures []creatureView       `json:"creatures"`
	}
	v := view{Seed: core.QuotedU64(world.Seed), Tick: world.Tick, GridSize: rulegarden.GridSize, Objects: world.Objects}
	for _, c := range world.Creatures {
		cv := creatureView{ID: c.ID, X: c.X, Y: c.Y, Decision: c.LastDecision}
		for _, rec := range c.Brain.Lessons() {
			cv.Lessons = append(cv.Lessons, lessonView{ID: rec.ID.String(), Label: rec.Label, Removed: rec.Removed})
		}
		v.Creatures = append(v.Creatures, cv)
	}
	return v
}

type lessonView struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Removed bool   `json:"removed"`
}

func main() {
	api := map[string]any{
		// newWorld(seedString) — fresh world from a seed.
		"newWorld": js.FuncOf(func(this js.Value, args []js.Value) any {
			seed, err := strconv.ParseUint(args[0].String(), 10, 64)
			if err != nil {
				return reply(nil, err)
			}
			world = rulegarden.NewWorld(seed)
			return reply(state(), nil)
		}),
		// tick(n) — advance n steps. Arguments arrive as strings (the JS shim stringifies
		// uniformly); js.Value.Int on a string panics the whole runtime, so parse instead.
		"tick": js.FuncOf(func(this js.Value, args []js.Value) any {
			n, err := strconv.Atoi(args[0].String())
			if err != nil {
				return reply(nil, err)
			}
			for i := 0; i < n; i++ {
				world.Step()
			}
			return reply(state(), nil)
		}),
		// state() — current render payload.
		"state": js.FuncOf(func(this js.Value, args []js.Value) any {
			return reply(state(), nil)
		}),
		// apply(eventJSON) — spawn/teach/transfer/forget, all through the event log so every
		// interaction is captured in the shareable pack.
		"apply": js.FuncOf(func(this js.Value, args []js.Value) any {
			var e rulegarden.Event
			if err := json.Unmarshal([]byte(args[0].String()), &e); err != nil {
				return reply(nil, err)
			}
			if err := world.Apply(e); err != nil {
				return reply(nil, err)
			}
			return reply(state(), nil)
		}),
		// generateIdentity() — mint a fresh ed25519 identity and activate it. Returns the
		// 32-byte seed (base64) for the page to persist; from now on receipts and exported
		// packs are signed.
		"generateIdentity": js.FuncOf(func(this js.Value, args []js.Value) any {
			_, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return reply(nil, err)
			}
			identity = priv
			info := identityInfo(priv)
			info["seed_b64"] = base64.StdEncoding.EncodeToString(priv.Seed())
			return reply(info, nil)
		}),
		// loadIdentity(seedB64) — activate a persisted identity (IndexedDB or an imported
		// key backup).
		"loadIdentity": js.FuncOf(func(this js.Value, args []js.Value) any {
			seed, err := base64.StdEncoding.DecodeString(args[0].String())
			if err != nil {
				return reply(nil, fmt.Errorf("invalid key backup: %w", err))
			}
			if len(seed) != ed25519.SeedSize {
				return reply(nil, fmt.Errorf("invalid key backup: seed is %d bytes, want %d", len(seed), ed25519.SeedSize))
			}
			identity = ed25519.NewKeyFromSeed(seed)
			return reply(identityInfo(identity), nil)
		}),
		// publicKey() — the active identity's public half + fingerprint.
		"publicKey": js.FuncOf(func(this js.Value, args []js.Value) any {
			if identity == nil {
				return reply(nil, fmt.Errorf("no identity loaded"))
			}
			return reply(identityInfo(identity), nil)
		}),
		// inspectPack(packJSON) — parse a pack WITHOUT applying it: signature state plus
		// shape summary. This is what the registry trust flow calls before deciding to
		// import/merge (the decision belongs to the player, so nothing mutates here).
		"inspectPack": js.FuncOf(func(this js.Value, args []js.Value) any {
			raw := []byte(args[0].String())
			if len(raw) > rulegarden.MaxPackBytes {
				return reply(nil, fmt.Errorf("pack is %d bytes, exceeding the %d-byte limit", len(raw), rulegarden.MaxPackBytes))
			}
			var p rulegarden.Pack
			if err := json.Unmarshal(raw, &p); err != nil {
				return reply(nil, fmt.Errorf("invalid pack: %w", err))
			}
			info := packSignatureInfo(p)
			info["seed"] = strconv.FormatUint(uint64(p.Seed), 10)
			info["ticks"] = p.Ticks
			info["events"] = len(p.Events)
			if len(p.PublicKey) > 0 {
				info["public_key_b64"] = base64.StdEncoding.EncodeToString(p.PublicKey)
			}
			return reply(info, nil)
		}),
		// exportPack() — the shareable seed+log world pack, signed when an identity is
		// active (signing is additive; unsigned export still replays identically).
		"exportPack": js.FuncOf(func(this js.Value, args []js.Value) any {
			p := world.Export()
			if identity != nil {
				p.Sign(identity)
			}
			data, err := json.Marshal(p)
			if err != nil {
				return reply(nil, err)
			}
			return reply(json.RawMessage(data), nil)
		}),
		// importPack(packJSON) — replay a shared world to a bit-identical state. A signed
		// pack whose signature does not match its content is refused by replay; the reply
		// surfaces the signature state {signed, valid, fingerprint} for the UI.
		"importPack": js.FuncOf(func(this js.Value, args []js.Value) any {
			raw := []byte(args[0].String())
			if len(raw) > rulegarden.MaxPackBytes {
				return reply(nil, fmt.Errorf("pack is %d bytes, exceeding the %d-byte limit", len(raw), rulegarden.MaxPackBytes))
			}
			var p rulegarden.Pack
			if err := json.Unmarshal(raw, &p); err != nil {
				return reply(nil, fmt.Errorf("invalid pack: %w", err))
			}
			w, err := rulegarden.Replay(p)
			if err != nil {
				return reply(nil, err)
			}
			world = w
			return reply(map[string]any{
				"state":          state(),
				"pack_signature": packSignatureInfo(p),
			}, nil)
		}),
		// hash() — the determinism fingerprint (two identical hashes = identical worlds).
		"hash": js.FuncOf(func(this js.Value, args []js.Value) any {
			return reply(world.Hash(), nil)
		}),
		// mergeBrains(packJSON, creatureID) — NeuroMesh: replay a friend's world pack and
		// merge every creature brain in it into the given local creature. Logged as an event
		// (the foreign pack rides along), so the merged world still replays bit-exactly.
		"mergeBrains": js.FuncOf(func(this js.Value, args []js.Value) any {
			var p rulegarden.Pack
			if err := json.Unmarshal([]byte(args[0].String()), &p); err != nil {
				return reply(nil, err)
			}
			id, err := strconv.Atoi(args[1].String())
			if err != nil {
				return reply(nil, err)
			}
			if err := world.MergeBrainsFrom(p, id); err != nil {
				return reply(nil, err)
			}
			return reply(map[string]any{
				"state":          state(),
				"pack_signature": packSignatureInfo(p),
			}, nil)
		}),
		// certify(creatureID) — ProofRoute: a replay-verifiable receipt for the creature's
		// last decision plus its brain image (base64 v3), the pair nvsa-verify consumes.
		// Signed with the active identity when one is loaded (verify strictly with
		// nvsa-verify -require-signature); unsigned receipts remain replay-verifiable.
		"certify": js.FuncOf(func(this js.Value, args []js.Value) any {
			id, err := strconv.Atoi(args[0].String())
			if err != nil {
				return reply(nil, err)
			}
			cert, brain, err := world.CertifyCreature(id)
			if err != nil {
				return reply(nil, err)
			}
			data := map[string]any{
				"receipt":   cert,
				"brain_b64": base64.StdEncoding.EncodeToString(brain),
			}
			if identity != nil {
				cert.Sign(identity)
				data["receipt"] = cert
				data["signer"] = engine.KeyFingerprint(cert.PublicKey)
			}
			return reply(data, nil)
		}),
		// ---- live sync (Phase C): flat lesson packs over a peer channel ----
		// brainPacks(creatureID) — the creature's active lessons as one pack per author
		// site (lossless for merged brains), each signed when an identity is active. This is
		// both the on-connect snapshot and the after-mutation broadcast.
		"brainPacks": js.FuncOf(func(this js.Value, args []js.Value) any {
			id, err := strconv.Atoi(args[0].String())
			if err != nil {
				return reply(nil, err)
			}
			packs, err := world.BrainPacks("live-sync", id)
			if err != nil {
				return reply(nil, err)
			}
			if identity != nil {
				for i := range packs {
					packs[i].Sign(identity)
				}
			}
			return reply(map[string]any{"packs": packs}, nil)
		}),
		// revocationPack(creatureID, lessonID) — the pack a peer needs to tombstone one
		// forgotten lesson (zero bound vector: revocation is by identity, not content).
		"revocationPack": js.FuncOf(func(this js.Value, args []js.Value) any {
			id, err := strconv.Atoi(args[0].String())
			if err != nil {
				return reply(nil, err)
			}
			var lesson engine.AssociationID
			if err := lesson.UnmarshalText([]byte(args[1].String())); err != nil {
				return reply(nil, fmt.Errorf("invalid lesson id: %w", err))
			}
			p, err := world.RevocationPack("live-revoke", id, lesson)
			if err != nil {
				return reply(nil, err)
			}
			if identity != nil {
				p.Sign(identity)
			}
			return reply(map[string]any{"pack": p}, nil)
		}),
		// inspectLessonPack(packJSON) — signature state + shape, WITHOUT applying. The
		// sync trust gate calls this before deciding.
		"inspectLessonPack": js.FuncOf(func(this js.Value, args []js.Value) any {
			var p engine.Pack
			if err := json.Unmarshal([]byte(args[0].String()), &p); err != nil {
				return reply(nil, fmt.Errorf("invalid lesson pack: %w", err))
			}
			info := map[string]any{
				"signed":  len(p.Signature) > 0,
				"entries": len(p.Entries),
				"site":    strconv.FormatUint(p.Site, 10),
			}
			if len(p.Signature) > 0 {
				info["valid"] = p.VerifySignature()
				info["fingerprint"] = engine.KeyFingerprint(p.PublicKey)
				info["public_key_b64"] = base64.StdEncoding.EncodeToString(p.PublicKey)
			}
			return reply(info, nil)
		}),
		// applyRemotePack(packJSON, creatureID) — the receive half of live sync: a logged,
		// replayable apply_pack event (idempotent merge; invalid signatures refused).
		"applyRemotePack": js.FuncOf(func(this js.Value, args []js.Value) any {
			var p engine.Pack
			if err := json.Unmarshal([]byte(args[0].String()), &p); err != nil {
				return reply(nil, fmt.Errorf("invalid lesson pack: %w", err))
			}
			id, err := strconv.Atoi(args[1].String())
			if err != nil {
				return reply(nil, err)
			}
			if err := world.ApplyLessonPackTo(p, id); err != nil {
				return reply(nil, err)
			}
			return reply(state(), nil)
		}),
		// applyRemoteRevoke(packJSON, creatureID) — a peer's forget, as a logged
		// revoke_pack event (tombstones propagate; idempotent).
		"applyRemoteRevoke": js.FuncOf(func(this js.Value, args []js.Value) any {
			var p engine.Pack
			if err := json.Unmarshal([]byte(args[0].String()), &p); err != nil {
				return reply(nil, fmt.Errorf("invalid lesson pack: %w", err))
			}
			id, err := strconv.Atoi(args[1].String())
			if err != nil {
				return reply(nil, err)
			}
			if err := world.RevokeLessonPackFrom(p, id); err != nil {
				return reply(nil, err)
			}
			return reply(state(), nil)
		}),
		// version() — engine capacity constant for UI limits.
		"version": js.FuncOf(func(this js.Value, args []js.Value) any {
			return reply(map[string]any{
				"maxLessons": engine.RecommendedMaxActiveAssociations,
			}, nil)
		}),
	}
	js.Global().Set("RuleGarden", js.ValueOf(api))
	js.Global().Get("console").Call("log", "RuleGarden wasm engine ready")
	select {} // keep the Go runtime alive for callbacks
}
