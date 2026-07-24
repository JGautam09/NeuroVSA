// Command nvsa-pack authors, publishes, and verifies entries in a NeuroVSA pack registry.
//
//	nvsa-pack keygen  -out key.json                                # mint a publishing identity
//	nvsa-pack sign    -key key.json -in world.json [-out signed.json]
//	nvsa-pack publish -key key.json -in world.json -name NAME -desc TEXT [-registry DIR]
//	nvsa-pack verify  [-registry DIR]                              # lint the whole registry
//
// A registry is a static directory — index.json (the manifest) plus packs/*.json — hosted
// anywhere that serves files (this repo on GitHub is the reference registry; publishing is
// a commit/PR, not an API call). The trust model, stated plainly: the ed25519 signature
// EMBEDDED in each pack is authoritative; the manifest is a browsing convenience and its
// sha256/author fields exist so tampering is caught early, never to replace verification.
//
// Key files use the exact backup format the RuleGarden browser exports
// ({"rulegarden_key":1,"seed_b64":...}), so a browser identity can publish from the CLI.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JGautam09/NeuroVSA/engine"
	"github.com/JGautam09/NeuroVSA/rulegarden"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "nvsa-pack: "+format+"\n", args...)
	os.Exit(1)
}

// keyFile is the browser's key-backup format, reused verbatim.
type keyFile struct {
	RuleGardenKey int    `json:"rulegarden_key"`
	SeedB64       string `json:"seed_b64"`
}

// manifest is registry/index.json. Entries are kept name-sorted so republishing is a
// deterministic, reviewable diff.
type manifest struct {
	Version int     `json:"version"`
	Packs   []entry `json:"packs"`
}

type entry struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	Kind              string `json:"kind"` // "world" | "lessons"
	File              string `json:"file"` // registry-relative, forward slashes
	Bytes             int    `json:"bytes"`
	SHA256            string `json:"sha256"`
	AuthorPublicKey   string `json:"author_public_key"` // base64; must equal the embedded key
	AuthorFingerprint string `json:"author_fingerprint"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: nvsa-pack <keygen|sign|publish|verify> [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "keygen":
		cmdKeygen(os.Args[2:])
	case "sign":
		cmdSign(os.Args[2:])
	case "publish":
		cmdPublish(os.Args[2:])
	case "verify":
		cmdVerify(os.Args[2:])
	default:
		fail("unknown subcommand %q (want keygen, sign, publish, or verify)", os.Args[1])
	}
}

// ---- keygen ----

func cmdKeygen(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "nvsa-key.json", "where to write the key file (keep it private)")
	fs.Parse(args)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fail("generate key: %v", err)
	}
	blob, _ := json.MarshalIndent(keyFile{RuleGardenKey: 1, SeedB64: base64.StdEncoding.EncodeToString(priv.Seed())}, "", "  ")
	// 0600: the seed IS the identity; nobody else on the machine needs to read it.
	if err := os.WriteFile(*out, append(blob, '\n'), 0o600); err != nil {
		fail("write key: %v", err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	fmt.Printf("key written to %s\n  fingerprint: %s\n  public key : %s\nAnyone holding this file can sign as you — store it privately.\n",
		*out, engine.KeyFingerprint(pub), base64.StdEncoding.EncodeToString(pub))
}

func loadKey(path string) ed25519.PrivateKey {
	raw, err := os.ReadFile(path)
	if err != nil {
		fail("read key: %v", err)
	}
	var kf keyFile
	if err := json.Unmarshal(raw, &kf); err != nil || kf.RuleGardenKey != 1 {
		fail("%s is not a rulegarden key backup", path)
	}
	seed, err := base64.StdEncoding.DecodeString(kf.SeedB64)
	if err != nil || len(seed) != ed25519.SeedSize {
		fail("%s holds an invalid seed", path)
	}
	return ed25519.NewKeyFromSeed(seed)
}

// ---- pack handling (both kinds) ----

// packKind sniffs which artifact a JSON file holds: RuleGarden world packs carry "events",
// engine lesson packs carry "entries".
func packKind(raw []byte) (string, error) {
	var probe struct {
		Events  *json.RawMessage `json:"events"`
		Entries *json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", fmt.Errorf("not valid JSON: %w", err)
	}
	switch {
	case probe.Events != nil:
		return "world", nil
	case probe.Entries != nil:
		return "lessons", nil
	default:
		return "", fmt.Errorf("neither a world pack (events) nor a lesson pack (entries)")
	}
}

// signRaw signs a pack of either kind and returns the re-marshaled JSON plus its embedded
// public key. World packs are fully replayed first — never sign what you haven't validated.
func signRaw(raw []byte, priv ed25519.PrivateKey) ([]byte, []byte, error) {
	kind, err := packKind(raw)
	if err != nil {
		return nil, nil, err
	}
	switch kind {
	case "world":
		if _, err := rulegarden.ImportJSON(raw); err != nil {
			return nil, nil, fmt.Errorf("world pack does not replay cleanly: %w", err)
		}
		var p rulegarden.Pack
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, nil, err
		}
		p.Sign(priv)
		out, err := json.MarshalIndent(p, "", " ")
		return out, p.PublicKey, err
	default: // lessons
		var p engine.Pack
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, nil, err
		}
		if _, err := p.Memory(); err != nil {
			return nil, nil, fmt.Errorf("lesson pack is not installable: %w", err)
		}
		p.Sign(priv)
		out, err := json.MarshalIndent(p, "", " ")
		return out, p.PublicKey, err
	}
}

// verifyRaw checks the embedded signature of a pack of either kind and returns
// (kind, embedded public key). World packs are also fully replayed.
func verifyRaw(raw []byte) (string, []byte, error) {
	kind, err := packKind(raw)
	if err != nil {
		return "", nil, err
	}
	switch kind {
	case "world":
		if _, err := rulegarden.ImportJSON(raw); err != nil {
			return kind, nil, fmt.Errorf("does not replay: %w", err)
		}
		var p rulegarden.Pack
		if err := json.Unmarshal(raw, &p); err != nil {
			return kind, nil, err
		}
		if len(p.Signature) == 0 {
			return kind, nil, fmt.Errorf("unsigned (registry packs must be signed)")
		}
		if !p.VerifySignature() {
			return kind, nil, fmt.Errorf("signature INVALID")
		}
		return kind, p.PublicKey, nil
	default:
		var p engine.Pack
		if err := json.Unmarshal(raw, &p); err != nil {
			return kind, nil, err
		}
		if len(p.Signature) == 0 {
			return kind, nil, fmt.Errorf("unsigned (registry packs must be signed)")
		}
		if !p.VerifySignature() {
			return kind, nil, fmt.Errorf("signature INVALID")
		}
		return kind, p.PublicKey, nil
	}
}

// ---- sign ----

func cmdSign(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyPath := fs.String("key", "", "key file (from keygen or a browser key export)")
	in := fs.String("in", "", "pack JSON to sign (world or lesson pack)")
	out := fs.String("out", "", "output path (default: overwrite -in)")
	fs.Parse(args)
	if *keyPath == "" || *in == "" {
		fail("sign requires -key and -in")
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fail("read pack: %v", err)
	}
	signed, pub, err := signRaw(raw, loadKey(*keyPath))
	if err != nil {
		fail("sign: %v", err)
	}
	dst := *out
	if dst == "" {
		dst = *in
	}
	if err := os.WriteFile(dst, append(signed, '\n'), 0o644); err != nil {
		fail("write signed pack: %v", err)
	}
	fmt.Printf("signed %s -> %s (author %s)\n", *in, dst, engine.KeyFingerprint(pub))
}

// ---- publish ----

func cmdPublish(args []string) {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	keyPath := fs.String("key", "", "key file; the pack is (re)signed with it")
	in := fs.String("in", "", "pack JSON to publish")
	name := fs.String("name", "", "registry name (unique; lowercase-slug recommended)")
	desc := fs.String("desc", "", "one-line description shown in browsers")
	regDir := fs.String("registry", "registry", "registry directory")
	fs.Parse(args)
	if *keyPath == "" || *in == "" || *name == "" {
		fail("publish requires -key, -in, and -name")
	}
	if strings.ContainsAny(*name, "/\\ ") {
		fail("name %q must be a slug (no spaces or slashes)", *name)
	}

	raw, err := os.ReadFile(*in)
	if err != nil {
		fail("read pack: %v", err)
	}
	signed, pub, err := signRaw(raw, loadKey(*keyPath))
	if err != nil {
		fail("publish: %v", err)
	}
	kind, _, err := verifyRaw(signed)
	if err != nil {
		fail("publish self-check: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(*regDir, "packs"), 0o755); err != nil {
		fail("create registry: %v", err)
	}
	rel := "packs/" + *name + ".json"
	file := filepath.Join(*regDir, filepath.FromSlash(rel))
	body := append(signed, '\n')
	if err := os.WriteFile(file, body, 0o644); err != nil {
		fail("write pack: %v", err)
	}

	m := readManifest(*regDir) // fresh registry -> empty manifest
	sum := sha256.Sum256(body)
	e := entry{
		Name: *name, Description: *desc, Kind: kind, File: rel,
		Bytes: len(body), SHA256: hex.EncodeToString(sum[:]),
		AuthorPublicKey:   base64.StdEncoding.EncodeToString(pub),
		AuthorFingerprint: engine.KeyFingerprint(pub),
	}
	replaced := false
	for i := range m.Packs {
		if m.Packs[i].Name == *name {
			m.Packs[i], replaced = e, true
		}
	}
	if !replaced {
		m.Packs = append(m.Packs, e)
	}
	sort.Slice(m.Packs, func(i, j int) bool { return m.Packs[i].Name < m.Packs[j].Name })
	writeManifest(*regDir, m)
	fmt.Printf("published %q (%s, %d bytes) by %s\n  %s\n  manifest: %s\n",
		*name, kind, len(body), e.AuthorFingerprint, file, filepath.Join(*regDir, "index.json"))
}

func readManifest(regDir string) manifest {
	m := manifest{Version: 1}
	raw, err := os.ReadFile(filepath.Join(regDir, "index.json"))
	if err != nil {
		return m // fresh registry
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		fail("existing index.json is corrupt: %v", err)
	}
	if m.Version != 1 {
		fail("unsupported registry version %d", m.Version)
	}
	return m
}

func writeManifest(regDir string, m manifest) {
	blob, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(regDir, "index.json"), append(blob, '\n'), 0o644); err != nil {
		fail("write manifest: %v", err)
	}
}

// ---- verify ----

func cmdVerify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	regDir := fs.String("registry", "registry", "registry directory")
	fs.Parse(args)

	if err := VerifyRegistry(*regDir, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "nvsa-pack: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("REGISTRY OK: every entry hashes, parses, verifies, and matches its manifest author.")
}

// VerifyRegistry lints a whole registry directory; it is the library entry point the tests
// (and CI, via the tests) run against the committed registry.
func VerifyRegistry(regDir string, w *os.File) error {
	raw, err := os.ReadFile(filepath.Join(regDir, "index.json"))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	if m.Version != 1 {
		return fmt.Errorf("unsupported registry version %d", m.Version)
	}
	seen := map[string]bool{}
	for _, e := range m.Packs {
		if seen[e.Name] {
			return fmt.Errorf("%s: duplicate name in manifest", e.Name)
		}
		seen[e.Name] = true
		if strings.Contains(e.File, "..") || strings.HasPrefix(e.File, "/") {
			return fmt.Errorf("%s: refusing path %q", e.Name, e.File)
		}
		body, err := os.ReadFile(filepath.Join(regDir, filepath.FromSlash(e.File)))
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		if len(body) != e.Bytes {
			return fmt.Errorf("%s: size %d != manifest %d", e.Name, len(body), e.Bytes)
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != e.SHA256 {
			return fmt.Errorf("%s: sha256 mismatch (file tampered or manifest stale)", e.Name)
		}
		kind, pub, err := verifyRaw(body)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		if kind != e.Kind {
			return fmt.Errorf("%s: kind %q != manifest %q", e.Name, kind, e.Kind)
		}
		if base64.StdEncoding.EncodeToString(pub) != e.AuthorPublicKey {
			return fmt.Errorf("%s: embedded key does not match manifest author (manifest tampered?)", e.Name)
		}
		fmt.Fprintf(w, "  ok  %-24s %-7s %6dB  by %s\n", e.Name, e.Kind, e.Bytes, e.AuthorFingerprint)
	}
	return nil
}
