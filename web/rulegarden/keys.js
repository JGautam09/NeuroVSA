// Identity custody for RuleGarden: a 32-byte ed25519 seed persisted in IndexedDB.
//
// Honest security model, stated plainly: IndexedDB is same-origin storage. Any script
// running on this origin (i.e. an XSS) could read the seed. That is acceptable for a game
// identity that signs world packs and decision receipts — do NOT reuse this key for
// anything valuable. Hardening (WebCrypto-wrapped, non-extractable keys) is a documented
// later step; the signature format itself would not change.
'use strict';

const KEYDB = { name: 'rulegarden-identity', store: 'keys', id: 'seed' };

function keydbOpen() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(KEYDB.name, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(KEYDB.store);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

function keydbGet() {
  return keydbOpen().then((db) => new Promise((resolve, reject) => {
    const req = db.transaction(KEYDB.store).objectStore(KEYDB.store).get(KEYDB.id);
    req.onsuccess = () => resolve(req.result || null);
    req.onerror = () => reject(req.error);
  }));
}

function keydbPut(seedB64) {
  return keydbOpen().then((db) => new Promise((resolve, reject) => {
    const tx = db.transaction(KEYDB.store, 'readwrite');
    tx.objectStore(KEYDB.store).put(seedB64, KEYDB.id);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  }));
}

// initIdentity(call): activate the stored identity, or mint + persist a fresh one.
// Returns {fingerprint, public_key_b64} — or null when key storage is unavailable
// (private browsing, etc.), in which case everything runs unsigned as before.
async function initIdentity(call) {
  try {
    const stored = await keydbGet();
    if (stored) {
      const info = call('loadIdentity', stored);
      if (info) return info;
      // A corrupt stored seed falls through to minting a fresh identity.
    }
    const fresh = call('generateIdentity');
    if (!fresh) return null;
    await keydbPut(fresh.seed_b64);
    return { fingerprint: fresh.fingerprint, public_key_b64: fresh.public_key_b64 };
  } catch (e) {
    return null;
  }
}

// exportIdentityBackup(): the seed as a small JSON backup the player downloads and keeps.
async function exportIdentityBackup() {
  const seed = await keydbGet();
  if (!seed) return null;
  return JSON.stringify({ rulegarden_key: 1, seed_b64: seed });
}

// importIdentityBackup(text, call): restore a backup — activates and persists the key.
async function importIdentityBackup(text, call) {
  let parsed;
  try {
    parsed = JSON.parse(text);
  } catch (e) {
    return null;
  }
  if (!parsed || parsed.rulegarden_key !== 1 || typeof parsed.seed_b64 !== 'string') return null;
  const info = call('loadIdentity', parsed.seed_b64);
  if (!info) return null;
  await keydbPut(parsed.seed_b64);
  return info;
}
