//go:build js && wasm

// Command rulegarden-wasm is the browser bridge for the RuleGarden world. It exposes a
// single global object `RuleGarden` whose methods take and return JSON strings — keeping the
// js.Value surface minimal and every payload inspectable in devtools. The entire engine runs
// inside the page: no server, no model file, no network calls.
package main

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"syscall/js"

	"github.com/JGautam09/NeuroVSA/engine"
	"github.com/JGautam09/NeuroVSA/rulegarden"
)

var world *rulegarden.World

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
		Seed      uint64               `json:"seed"`
		Tick      int                  `json:"tick"`
		GridSize  int                  `json:"grid_size"`
		Objects   []*rulegarden.Object `json:"objects"`
		Creatures []creatureView       `json:"creatures"`
	}
	v := view{Seed: world.Seed, Tick: world.Tick, GridSize: rulegarden.GridSize, Objects: world.Objects}
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
		// exportPack() — the shareable seed+log world pack.
		"exportPack": js.FuncOf(func(this js.Value, args []js.Value) any {
			data, err := world.ExportJSON()
			if err != nil {
				return reply(nil, err)
			}
			return reply(json.RawMessage(data), nil)
		}),
		// importPack(packJSON) — replay a shared world to a bit-identical state.
		"importPack": js.FuncOf(func(this js.Value, args []js.Value) any {
			w, err := rulegarden.ImportJSON([]byte(args[0].String()))
			if err != nil {
				return reply(nil, err)
			}
			world = w
			return reply(state(), nil)
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
			return reply(state(), nil)
		}),
		// certify(creatureID) — ProofRoute: a replay-verifiable receipt for the creature's
		// last decision plus its brain image (base64 v3), the pair nvsa-verify consumes.
		"certify": js.FuncOf(func(this js.Value, args []js.Value) any {
			id, err := strconv.Atoi(args[0].String())
			if err != nil {
				return reply(nil, err)
			}
			cert, brain, err := world.CertifyCreature(id)
			if err != nil {
				return reply(nil, err)
			}
			return reply(map[string]any{
				"receipt":   cert,
				"brain_b64": base64.StdEncoding.EncodeToString(brain),
			}, nil)
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
