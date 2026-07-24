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
  // Mutual-approval handshake state. NO brain data (snapshot, teach, forget) is sent OR
  // applied until BOTH sides reach `ready`. This closes the window where anyone who guessed
  // a room code received the brain snapshot before the legitimate user could reject them.
  localApproved: false, // we approved the peer's identity
  peerApproved: false,  // the peer sent us an accept
  ready: false,         // both approved → data may flow
  onLog: (m) => console.log('[sync]', m),
  onState: () => {}, // set by app.js: status chip
  onApply: null, // set by app.js: refresh UI after remote apply
  callEngine: null, // set by app.js: the {ok,data|error} bridge caller
};

function syncResetHandshake() {
  liveSync.peerFp = null;
  liveSync.peerPub = null;
  liveSync.localApproved = false;
  liveSync.peerApproved = false;
  liveSync.ready = false;
}

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
    // NOTHING else is sent yet. The brain snapshot goes out only after mutual approval
    // (syncMaybeReady), so an unapproved peer never receives it.
  };
  liveSync.dc.onclose = () => { liveSync.onLog('peer disconnected'); liveSync.onState('offline'); syncResetHandshake(); };
  liveSync.dc.onmessage = (ev) => {
    let msg;
    try { msg = JSON.parse(ev.data); } catch (e) { liveSync.onLog('✗ undecodable peer message dropped'); return; }
    try { syncHandle(msg); } catch (e) { liveSync.onLog('✗ ' + e.message); }
  };
}

// syncMaybeReady: once BOTH sides have approved, transition to ready and send our initial
// brain snapshot — never before. Idempotent.
function syncMaybeReady() {
  if (liveSync.ready || !liveSync.localApproved || !liveSync.peerApproved) return;
  liveSync.ready = true;
  liveSync.onLog(`synced with peer ${liveSync.peerFp} — brains now exchange`);
  liveSync.onState(`live with ${liveSync.peerFp}`);
  syncBroadcastBrain(); // the first snapshot leaves only after mutual approval
}

function syncHandle(msg) {
  if (msg.t === 'hello') {
    // Record the peer's advertised identity, then ask the LOCAL user to approve before any
    // data flows. Approving sends an `accept`; both sides must accept to become ready.
    liveSync.peerFp = msg.fp;
    liveSync.peerPub = msg.pub || null;
    if (!confirm(`Peer connected.\n\n  signing identity: ${msg.fp}\n\nSync brains with this peer? Their lessons will flow into your selected creature, and yours into theirs — only after you both agree.`)) {
      liveSync.dc.send(JSON.stringify({ t: 'decline' }));
      liveSync.onLog('declined the peer; disconnecting');
      liveSync.dc.close();
      return;
    }
    liveSync.localApproved = true;
    liveSync.dc.send(JSON.stringify({ t: 'accept' }));
    liveSync.onState(`awaiting ${msg.fp}…`);
    syncMaybeReady();
    return;
  }
  if (msg.t === 'accept') {
    if (liveSync.peerFp === null) throw new Error('accept before hello — dropped');
    liveSync.peerApproved = true;
    syncMaybeReady();
    return;
  }
  if (msg.t === 'decline') {
    liveSync.onLog('peer declined the connection');
    liveSync.dc.close();
    return;
  }

  // Everything below is brain data. It is refused until the handshake completes.
  if (!liveSync.ready) throw new Error(`peer sent ${msg.t} before both sides approved — dropped`);

  const gate = (packJSON) => {
    const info = liveSync.callEngine('inspectLessonPack', packJSON);
    if (!info) throw new Error('uninspectable pack dropped');
    // Once a peer advertised a public key at hello, EVERY pack from this connection must be
    // signed by exactly that key — unsigned or mismatched packs are refused. (A peer with no
    // identity advertised 'unsigned' at hello; the user approved that explicitly.)
    if (liveSync.peerPub) {
      if (!info.signed) throw new Error(`peer advertised a key but sent an UNSIGNED pack — dropped`);
      if (info.valid === false) throw new Error('pack signature INVALID — dropped');
      if (info.public_key_b64 !== liveSync.peerPub) {
        throw new Error(`pack signed by ${info.fingerprint}, not the connected peer ${liveSync.peerFp} — dropped`);
      }
    } else if (info.signed && info.valid === false) {
      throw new Error('pack signature INVALID — dropped');
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
// that asymmetry is what prevents echo loops; merges are idempotent anyway). Gated on
// `ready`: no brain data leaves until both sides approved the handshake.
function syncBroadcastBrain() {
  if (!syncConnected() || !liveSync.ready || liveSync.creature == null) return;
  const data = liveSync.callEngine('brainPacks', liveSync.creature);
  if (data) liveSync.dc.send(JSON.stringify({ t: 'packs', packs: data.packs || [] }));
}

function syncBroadcastRevoke(lessonID) {
  if (!syncConnected() || !liveSync.ready || liveSync.creature == null) return;
  const data = liveSync.callEngine('revocationPack', liveSync.creature, lessonID);
  if (data) liveSync.dc.send(JSON.stringify({ t: 'revoke', pack: data.pack }));
}
