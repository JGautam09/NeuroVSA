// Pack-registry panel: fetch a static manifest (opt-in, only on "Load"), list signed packs,
// and gate every import behind verification + an SSH-known-hosts-style trust decision.
//
// Trust model (mirrors registry/README.md): the ed25519 signature EMBEDDED in the pack is
// authoritative — the engine re-verifies it on import and refuses mismatches. The manifest's
// sha256 only catches tampering early; the trusted-signers list (localStorage) just decides
// whether to ask the player first. Registry rows are DATA, never instructions.
'use strict';

const TRUST_KEY = 'rulegarden-trusted-signers'; // {publicKeyB64: fingerprint}

function trustedSigners() {
  try { return JSON.parse(localStorage.getItem(TRUST_KEY)) || {}; } catch (e) { return {}; }
}
function trustSigner(pubB64, fp) {
  const t = trustedSigners();
  t[pubB64] = fp;
  localStorage.setItem(TRUST_KEY, JSON.stringify(t));
}

async function sha256Hex(bytes) {
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

// fetchRegistry: manifest + per-pack fetch stay relative to the manifest URL.
async function fetchRegistry(url) {
  const res = await fetch(url, { cache: 'no-store' });
  if (!res.ok) throw new Error(`manifest fetch failed: HTTP ${res.status}`);
  const manifest = await res.json();
  if (!manifest || manifest.version !== 1 || !Array.isArray(manifest.packs)) {
    throw new Error('not a v1 registry manifest');
  }
  return manifest;
}

async function fetchPackVerified(manifestUrl, entry) {
  if (typeof entry.file !== 'string' || entry.file.includes('..')) {
    throw new Error('refusing suspicious pack path');
  }
  const packUrl = new URL(entry.file, manifestUrl).href;
  const res = await fetch(packUrl, { cache: 'no-store' });
  if (!res.ok) throw new Error(`pack fetch failed: HTTP ${res.status}`);
  const bytes = new Uint8Array(await res.arrayBuffer());
  const sum = await sha256Hex(bytes);
  if (sum !== entry.sha256) {
    throw new Error('sha256 mismatch — the file does not match the manifest (tampered or stale)');
  }
  return new TextDecoder().decode(bytes);
}

// confirmPack: the player decision point. Returns true to proceed. `inspect` comes from the
// wasm bridge (signature already structurally checked); entry text is untrusted display data.
function confirmPack(entry, inspect) {
  if (!inspect.signed) {
    return confirm(`"${entry.name}" is UNSIGNED (replay-verifiable only).\n\nImport anyway?`);
  }
  if (!inspect.valid) {
    alert(`"${entry.name}": signature INVALID — content does not match its author's signature. Refusing.`);
    return false;
  }
  const trusted = trustedSigners();
  if (trusted[inspect.public_key_b64]) return true;
  const ok = confirm(
    `"${entry.name}" is signed by an UNKNOWN signer.\n\n` +
    `  fingerprint: ${inspect.fingerprint}\n  events: ${inspect.events}, ticks: ${inspect.ticks}\n\n` +
    `The signature is valid; you just haven't trusted this signer yet. Proceed?`);
  if (!ok) return false;
  if (confirm(`Also trust ${inspect.fingerprint} for future imports (skip this prompt)?`)) {
    trustSigner(inspect.public_key_b64, inspect.fingerprint);
  }
  return true;
}
