// Live world sync (Phase C): a WebRTC data channel between two players — world data never
// touches a server. Signaling is manual copy/paste (the same trust gesture as pasting a
// world pack; no signaling server), with public STUN for NAT traversal. Honest caveat: no
// TURN relay, so some symmetric-NAT networks won't connect — documented, not hidden.
//
// Protocol (all JSON): {t:'hello', fp, pub} once on open, then
//   {t:'packs',  packs:[<engine.Pack>...]}   full brain snapshot / after-mutation broadcast
//   {t:'revoke', pack:<engine.Pack>}         a forget, as a revocation pack
// Receivers apply packs via logged apply_pack / revoke_pack world events, so a live-synced
// world still replays bit-exactly. Merges are idempotent CRDT ops: re-sending a snapshot is
// a no-op, and message order between mutations doesn't matter.
//
// Identity gate: the peer's hello fingerprint is confirmed by the player once per
// connection. Every subsequent SIGNED pack must carry that same key (mismatch = refused);
// the engine independently refuses any signed pack whose signature doesn't verify.
'use strict';

const liveSync = {
  pc: null, dc: null,
  peerFp: null, peerPub: null,
  creature: null, // the local creature that learns from (and teaches) the peer
  onLog: (m) => console.log('[sync]', m),
  onState: () => {}, // set by app.js: status chip
  onApply: null, // set by app.js: refresh UI after remote apply
  callEngine: null, // set by app.js: the {ok,data|error} bridge caller
};

const SYNC_RTC = { iceServers: [{ urls: ['stun:stun.l.google.com:19302', 'stun:stun1.l.google.com:19302'] }] };

function syncEncode(desc) { return btoa(JSON.stringify(desc)); }
function syncDecode(text) { return JSON.parse(atob(text.trim())); }

// Gathers ICE candidates to completion so one paste carries the full description.
function syncGathered(pc) {
  return new Promise((resolve) => {
    if (pc.iceGatheringState === 'complete') return resolve();
    const check = () => { if (pc.iceGatheringState === 'complete') { pc.removeEventListener('icegatheringstatechange', check); resolve(); } };
    pc.addEventListener('icegatheringstatechange', check);
    setTimeout(resolve, 3000); // fallback: whatever gathered by now
  });
}

async function syncHost(creatureID) {
  liveSync.creature = creatureID;
  liveSync.pc = new RTCPeerConnection(SYNC_RTC);
  liveSync.dc = liveSync.pc.createDataChannel('rulegarden-sync');
  syncWireChannel();
  await liveSync.pc.setLocalDescription(await liveSync.pc.createOffer());
  await syncGathered(liveSync.pc);
  return syncEncode(liveSync.pc.localDescription);
}

async function syncJoin(offerText, creatureID) {
  liveSync.creature = creatureID;
  liveSync.pc = new RTCPeerConnection(SYNC_RTC);
  liveSync.pc.ondatachannel = (ev) => { liveSync.dc = ev.channel; syncWireChannel(); };
  await liveSync.pc.setRemoteDescription(syncDecode(offerText));
  await liveSync.pc.setLocalDescription(await liveSync.pc.createAnswer());
  await syncGathered(liveSync.pc);
  return syncEncode(liveSync.pc.localDescription);
}

async function syncAcceptAnswer(answerText) {
  await liveSync.pc.setRemoteDescription(syncDecode(answerText));
}

function syncConnected() { return liveSync.dc && liveSync.dc.readyState === 'open'; }

function syncWireChannel() {
  liveSync.dc.onopen = () => {
    liveSync.onLog('peer channel open — introducing ourselves');
    const me = liveSync.callEngine('publicKey');
    liveSync.dc.send(JSON.stringify({ t: 'hello', fp: me ? me.fingerprint : 'unsigned', pub: me ? me.public_key_b64 : '' }));
    syncBroadcastBrain(); // on-connect snapshot
  };
  liveSync.dc.onclose = () => { liveSync.onLog('peer disconnected'); liveSync.onState('offline'); liveSync.peerFp = null; liveSync.peerPub = null; };
  liveSync.dc.onmessage = (ev) => {
    let msg;
    try { msg = JSON.parse(ev.data); } catch (e) { liveSync.onLog('✗ undecodable peer message dropped'); return; }
    try { syncHandle(msg); } catch (e) { liveSync.onLog('✗ ' + e.message); }
  };
}

function syncHandle(msg) {
  if (msg.t === 'hello') {
    // The one identity decision per connection. Manual signaling already authenticated the
    // channel out-of-band; this pins which KEY the channel speaks for.
    if (!confirm(`Peer connected.\n\n  signing identity: ${msg.fp}\n\nSync brains with this peer? (their lessons flow into your selected creature, yours into theirs)`)) {
      liveSync.dc.close();
      return;
    }
    liveSync.peerFp = msg.fp;
    liveSync.peerPub = msg.pub || null;
    liveSync.onLog(`synced with peer ${msg.fp}`);
    liveSync.onState(`live with ${msg.fp}`);
    return;
  }
  if (liveSync.peerFp === null) throw new Error('peer message before hello — dropped');

  const gate = (packJSON) => {
    const info = liveSync.callEngine('inspectLessonPack', packJSON);
    if (!info) throw new Error('uninspectable pack dropped');
    if (info.signed && info.valid === false) throw new Error('pack signature INVALID — dropped');
    if (info.signed && liveSync.peerPub && info.public_key_b64 !== liveSync.peerPub) {
      throw new Error(`pack signed by ${info.fingerprint}, not the connected peer ${liveSync.peerFp} — dropped`);
    }
    return info;
  };

  if (msg.t === 'packs' && Array.isArray(msg.packs)) {
    for (const pack of msg.packs) {
      const packJSON = JSON.stringify(pack);
      gate(packJSON);
      const data = liveSync.callEngine('applyRemotePack', packJSON, liveSync.creature);
      if (data && liveSync.onApply) liveSync.onApply(data);
    }
    liveSync.onLog(`peer update: ${msg.packs.length} pack(s) merged into creature #${liveSync.creature}`);
    return;
  }
  if (msg.t === 'revoke' && msg.pack) {
    const packJSON = JSON.stringify(msg.pack);
    gate(packJSON);
    const data = liveSync.callEngine('applyRemoteRevoke', packJSON, liveSync.creature);
    if (data && liveSync.onApply) liveSync.onApply(data);
    liveSync.onLog(`peer forgot a lesson — tombstoned here too`);
    return;
  }
  throw new Error(`unknown peer message type ${msg.t} — dropped`);
}

// syncAutoConnect: the optional room-code path — a tiny signaling relay (`/signal` on the
// NeuroVSA api server) ferries the SAME base64 blobs the manual flow copy/pastes, and
// nothing else. First peer in the room hosts; second joins; the relay is closed as soon as
// the data channel opens. Resolves with our role, or rejects with a reason.
function syncAutoConnect(signalURL, roomCode, creatureID) {
  return new Promise((resolve, reject) => {
    let role = null, settled = false, ws;
    const finish = (err) => {
      if (settled) return;
      settled = true;
      clearInterval(watch);
      clearTimeout(deadline);
      try { if (ws && ws.readyState <= 1) ws.close(); } catch (e) { /* already closed */ }
      if (err) reject(err); else resolve(role);
    };
    try {
      ws = new WebSocket(signalURL + '?room=' + encodeURIComponent(roomCode));
    } catch (e) {
      return reject(new Error('bad signal URL: ' + e.message));
    }
    ws.onerror = () => finish(new Error('signal relay unreachable (is the api server running?)'));
    ws.onclose = () => { if (!syncConnected()) finish(new Error('signal channel closed before pairing')); };
    ws.onmessage = async (ev) => {
      const raw = ev.data;
      try {
        if (typeof raw === 'string' && raw[0] === '{') { // control frame from the relay
          const m = JSON.parse(raw);
          if (m.type === 'role') { role = m.role; liveSync.onLog(`signal: room "${roomCode}", we are ${role}`); return; }
          if (m.type === 'peer-joined' && role === 'host') { ws.send(await syncHost(creatureID)); return; }
          if (m.type === 'error') { finish(new Error('relay: ' + m.error)); return; }
          return; // peer-left etc.
        }
        // Opaque blob: the offer if we're joining, the answer if we're hosting.
        if (role === 'join') ws.send(await syncJoin(raw, creatureID));
        else await syncAcceptAnswer(raw);
      } catch (e) { finish(e); }
    };
    // Success = the data channel actually opened; the relay is then no longer needed.
    const watch = setInterval(() => { if (syncConnected()) finish(); }, 250);
    const deadline = setTimeout(() => finish(new Error('auto-connect timed out (strict NAT? use the manual blob flow)')), 20000);
  });
}

// Broadcast the local creature's brain after a LOCAL mutation (never after a remote apply —
// that asymmetry is what prevents echo loops; merges are idempotent anyway).
function syncBroadcastBrain() {
  if (!syncConnected() || liveSync.creature == null) return;
  const data = liveSync.callEngine('brainPacks', liveSync.creature);
  if (data) liveSync.dc.send(JSON.stringify({ t: 'packs', packs: data.packs || [] }));
}

function syncBroadcastRevoke(lessonID) {
  if (!syncConnected() || liveSync.creature == null) return;
  const data = liveSync.callEngine('revocationPack', liveSync.creature, lessonID);
  if (data) liveSync.dc.send(JSON.stringify({ t: 'revoke', pack: data.pack }));
}
