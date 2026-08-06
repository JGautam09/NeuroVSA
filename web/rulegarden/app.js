// RuleGarden front-end (deliberately plain: the G1 gate wants ugly-but-playable).
// All world logic lives in the wasm engine; this file only renders state and sends events.
'use strict';

const CELL = 20; // 24 × 20px = 480
const COLORS = {
  creature: '#7dffb0', food: '#ffd479', predator: '#ff6b6b', guard: '#6bb5ff',
  prey: '#d4a5ff', intruder: '#ff9f43', water: '#4dd0e1',
};
const KINDS = ['food', 'predator', 'guard', 'prey', 'intruder', 'water'];

let state = null;        // last render payload from the engine
let selected = null;     // selected creature id
let playTimer = null;

const $ = (id) => document.getElementById(id);
const logEl = $('log');

function log(msg) {
  logEl.textContent += msg + '\n';
  logEl.scrollTop = logEl.scrollHeight;
}

// el builds a DOM element with text set via textContent — NEVER innerHTML. Every value that
// can originate in an imported or peer-delivered pack (lesson labels, contributor labels,
// ids) flows through here, so an HTML/SVG/event-handler payload in a label renders as inert
// text and can never execute. This is the structural defense behind the P0 XSS fix (a
// stored-XSS label could otherwise read the ed25519 signing seed from same-origin IndexedDB).
function el(tag, opts = {}, children = []) {
  const n = document.createElement(tag);
  if (opts.text != null) n.textContent = opts.text; // textContent, not innerHTML
  if (opts.className) n.className = opts.className;
  for (const [k, v] of Object.entries(opts.attrs || {})) n.setAttribute(k, v);
  for (const c of children) n.appendChild(c);
  return n;
}
function clear(node) { while (node.firstChild) node.removeChild(node.firstChild); }

// ---- engine calls (uniform {ok, data|error} envelope) ----
function call(fn, ...args) {
  const raw = window.RuleGarden[fn](...args.map(String));
  const env = JSON.parse(raw);
  if (!env.ok) {
    log('✗ ' + env.error);
    return null;
  }
  return env.data;
}

function refresh(data) {
  if (data) state = data;
  render();
}

// ---- rendering ----
function render() {
  if (!state) return;
  $('tickNo').textContent = state.tick;
  const ctx = $('grid').getContext('2d');
  ctx.clearRect(0, 0, 480, 480);

  ctx.strokeStyle = '#12241a';
  for (let i = 0; i <= state.grid_size; i++) {
    ctx.beginPath(); ctx.moveTo(i * CELL, 0); ctx.lineTo(i * CELL, 480); ctx.stroke();
    ctx.beginPath(); ctx.moveTo(0, i * CELL); ctx.lineTo(480, i * CELL); ctx.stroke();
  }

  for (const o of state.objects || []) {
    if (!o.alive) continue;
    ctx.fillStyle = COLORS[o.kind] || '#888';
    ctx.fillRect(o.x * CELL + 4, o.y * CELL + 4, CELL - 8, CELL - 8);
  }
  for (const c of state.creatures || []) {
    ctx.fillStyle = COLORS.creature;
    ctx.beginPath();
    ctx.arc(c.x * CELL + CELL / 2, c.y * CELL + CELL / 2, CELL / 2 - 3, 0, Math.PI * 2);
    ctx.fill();
    if (c.id === selected) {
      ctx.strokeStyle = '#fff';
      ctx.lineWidth = 2;
      ctx.stroke();
      ctx.lineWidth = 1;
    }
  }
  renderInspector();
}

function renderInspector() {
  const cr = (state.creatures || []).find((c) => c.id === selected);
  $('noSelection').style.display = cr ? 'none' : '';
  $('selection').style.display = cr ? '' : 'none';
  if (!cr) return;

  $('selId').textContent = '#' + cr.id;

  // Decision panel — built entirely from DOM nodes; percept/action are engine enums but
  // contributor labels are untrusted, so everything goes through el()/textContent.
  const dec = $('decision');
  clear(dec);
  const d = cr.decision;
  if (d) {
    const p = d.percept.sees === 'nothing' ? 'nothing' : `${d.percept.sees}, ${d.percept.dist}, ${d.percept.dir}`;
    dec.appendChild(document.createTextNode('sees '));
    dec.appendChild(el('b', { text: p }));
    dec.appendChild(document.createTextNode(' → '));
    dec.appendChild(el('b', { text: d.action }));
    dec.appendChild(document.createTextNode(' ('));
    dec.appendChild(el('span', { text: d.basis, className: 'basis-' + d.basis }));
    dec.appendChild(document.createTextNode(`, margin ${d.margin})`));

    const table = el('table', {}, [
      el('tr', {}, [el('th', { text: 'action' }), el('th', { text: 'distance' })]),
      ...d.candidates.map((c) => el('tr', {}, [el('td', { text: c.token }), el('td', { text: String(c.distance) })])),
    ]);
    dec.appendChild(table);

    for (const c of d.contributors || []) {
      dec.appendChild(el('div', {}, [
        el('span', { text: '↳ memory: ', className: 'muted' }),
        // c.label is untrusted pack content — textContent, never markup.
        el('span', { text: c.label || '#' + c.id }),
      ]));
    }
  } else {
    dec.appendChild(el('span', { text: 'no decision yet — tick the world', className: 'muted' }));
  }

  // Lessons ledger — DOM rows, everything through el()/textContent (labels are untrusted).
  // A structured lesson renders its MACHINE-VERIFIED meaning (percept → action from the
  // sem record, checked against the bound vector at import); its label appears only as a
  // nickname when it differs. Legacy (pre-sem) imports render their label with a marker —
  // the label was validated at import, but such lessons cannot transfer.
  const lessons = $('lessons');
  clear(lessons);
  lessons.appendChild(el('tr', {}, [el('th', { text: 'id' }), el('th', { text: 'lesson' }), el('th')]));
  for (const l of cr.lessons || []) {
    const actionCell = el('td');
    if (!l.removed) {
      const btn = el('button', { text: 'forget' });
      btn.onclick = () => {
        refresh(call('apply', JSON.stringify({ op: 'forget', creature: selected, lesson: l.id })));
        log(`forgot lesson ${l.id} — memory is now bit-identical to never having learned it`);
        syncBroadcastRevoke(l.id); // live peers tombstone it too
      };
      actionCell.appendChild(btn);
    }
    const lessonCell = el('td', { className: l.removed ? 'removed' : '' });
    if (l.structured) {
      const canonical = `${l.percept} → ${l.action}`;
      lessonCell.appendChild(el('span', { text: canonical }));
      if (l.parent) lessonCell.appendChild(el('span', { text: ` (from ${l.parent})`, className: 'muted' }));
      if (l.label && l.label !== canonical && !l.label.startsWith(canonical)) {
        lessonCell.appendChild(el('div', { text: `“${l.label}”`, className: 'muted' }));
      }
    } else {
      lessonCell.appendChild(el('span', { text: l.label }));
      lessonCell.appendChild(el('span', { text: ' [legacy — no verified semantics]', className: 'muted' }));
    }
    lessons.appendChild(el('tr', { className: 'lesson-row' }, [
      el('td', { text: l.id }),
      lessonCell,
      actionCell,
    ]));
  }

  // Transfer select — structured lessons only carry transferable semantics; legacy entries
  // are listed disabled so the refusal is visible before the click, not after.
  const xLesson = $('xLesson');
  clear(xLesson);
  for (const l of (cr.lessons || []).filter((x) => !x.removed)) {
    const text = l.structured
      ? `#${l.id} ${l.percept} → ${l.action}`
      : `#${l.id} ${l.label.slice(0, 40)} (legacy — re-teach to transfer)`;
    const opt = el('option', { text, attrs: { value: l.id } });
    if (!l.structured) opt.setAttribute('disabled', '');
    xLesson.appendChild(opt);
  }
}

// ---- controls ----
$('newWorld').onclick = () => {
  refresh(call('newWorld', $('seed').value));
  selected = null;
  log(`new world, seed ${$('seed').value} — same seed + same actions ⇒ identical world, anywhere`);
};
$('tick1').onclick = () => refresh(call('tick', 1));
$('tick10').onclick = () => refresh(call('tick', 10));
$('play').onclick = () => {
  if (playTimer) {
    clearInterval(playTimer); playTimer = null; $('play').textContent = '▶ Play';
  } else {
    playTimer = setInterval(() => refresh(call('tick', 1)), 300);
    $('play').textContent = '⏸ Pause';
  }
};

$('grid').onclick = (ev) => {
  const rect = $('grid').getBoundingClientRect();
  const x = Math.floor((ev.clientX - rect.left) / CELL);
  const y = Math.floor((ev.clientY - rect.top) / CELL);

  // clicking a creature selects it
  const hit = (state.creatures || []).find((c) => c.x === x && c.y === y);
  if (hit) { selected = hit.id; render(); return; }

  const kind = $('spawnKind').value;
  if (kind === 'creature') {
    refresh(call('apply', JSON.stringify({ op: 'spawn_creature', x, y })));
    selected = state.creatures[state.creatures.length - 1].id;
    log(`spawned creature #${selected} at (${x},${y}) — empty brain, pure instinct`);
  } else {
    refresh(call('apply', JSON.stringify({ op: 'spawn_object', kind, x, y })));
    log(`spawned ${kind} at (${x},${y})`);
  }
};

$('teach').onclick = () => {
  if (!selected) return;
  const percept = { sees: $('tSees').value, dist: $('tDist').value, dir: $('tDir').value };
  refresh(call('apply', JSON.stringify({ op: 'teach', creature: selected, percept, action: $('tAct').value })));
  log(`taught #${selected}: see ${percept.sees} (${percept.dist}, ${percept.dir}) → ${$('tAct').value} — one-shot, no training loop`);
  syncBroadcastBrain(); // live peers learn it too
};

$('teachCurrent').onclick = () => {
  const cr = (state.creatures || []).find((c) => c.id === selected);
  if (!cr || !cr.decision || cr.decision.percept.sees === 'nothing') {
    log('creature has no usable current percept — tick the world first');
    return;
  }
  const p = cr.decision.percept;
  refresh(call('apply', JSON.stringify({ op: 'teach', creature: selected, percept: p, action: $('tAct').value })));
  log(`taught #${selected} from its own situation: ${p.sees} (${p.dist}, ${p.dir}) → ${$('tAct').value}`);
  syncBroadcastBrain(); // live peers learn it too
};

$('transfer').onclick = () => {
  if (!selected) return;
  const lesson = $('xLesson').value;
  refresh(call('apply', JSON.stringify({ op: 'transfer', creature: selected, lesson, new_sees: $('xSees').value })));
  log(`transferred lesson ${lesson} by analogy → subject ${$('xSees').value} (the dollar-of-Mexico move)`);
  syncBroadcastBrain(); // live peers learn it too
};

$('export').onclick = () => {
  const pack = call('exportPack');
  $('pack').value = JSON.stringify(pack);
  log(`exported world: seed + ${pack.events.length} events — that IS the whole world`);
};
$('import').onclick = () => {
  const data = call('importPack', $('pack').value);
  if (!data) return;
  refresh(data.state);
  selected = null;
  log('imported pack and replayed it deterministically — ' + describeSig(data.pack_signature));
};

// NeuroMesh in costume: merge every creature brain from the pasted world pack into the
// selected creature. The merge is logged as an event (the foreign pack rides along), so
// this world still replays bit-exactly — and foreign lessons keep their foreign site ids.
$('mergeBrains').onclick = () => {
  if (!selected) { log('select a creature first — it will absorb the visiting lessons'); return; }
  const data = call('mergeBrains', $('pack').value, selected);
  if (data) {
    refresh(data.state);
    log(`merged brains from the pasted world into creature #${selected} — foreign lessons keep their site ids (see ledger); ` + describeSig(data.pack_signature));
  }
};

// ProofRoute in costume: download a replay-verifiable receipt for the selected creature's
// last decision, plus its brain image. Verify anywhere:
//   nvsa-verify -cert receipt.json -memory brain.bin
$('receipt').onclick = () => {
  if (!selected) { log('select a creature first'); return; }
  const data = call('certify', selected);
  if (!data) return;
  download(`creature${selected}-receipt.json`, JSON.stringify(data.receipt, null, 1), 'application/json');
  download(`creature${selected}-brain.bin`, b64ToBytes(data.brain_b64), 'application/octet-stream');
  const signedNote = data.signer
    ? `SIGNED by ${data.signer} — verify strictly with -require-signature`
    : 'unsigned (replay-verifiable only)';
  log(`receipt + brain downloaded (${signedNote}) — nvsa-verify -cert creature${selected}-receipt.json -memory creature${selected}-brain.bin`);
};

$('hash').onclick = () => log('world hash: ' + call('hash'));

// ---- pack registry ----
$('regLoad').onclick = async () => {
  // Absolutize once (relative inputs like /registry/index.json are valid); every pack
  // fetch below resolves against this absolute manifest URL.
  const url = new URL($('regUrl').value.trim(), location.href).href;
  const listEl = $('regList');
  listEl.textContent = 'loading…';
  try {
    const manifest = await fetchRegistry(url);
    clear(listEl);
    if (!manifest.packs.length) { listEl.textContent = 'registry is empty'; return; }
    for (const entry of manifest.packs) {
      const row = document.createElement('div');
      row.style.margin = '3px 0';
      const label = document.createElement('span');
      // textContent only: registry strings are untrusted data, never markup.
      label.textContent = `${entry.name} (${entry.kind}, ${entry.bytes}B) by ${entry.author_fingerprint} — ${entry.description || ''} `;
      row.appendChild(label);
      if (entry.kind === 'world') {
        const imp = document.createElement('button');
        imp.textContent = 'Import';
        imp.onclick = () => applyRegistryPack(url, entry, 'import');
        const mrg = document.createElement('button');
        mrg.textContent = 'Merge→selected';
        mrg.style.marginLeft = '4px';
        mrg.onclick = () => applyRegistryPack(url, entry, 'merge');
        row.appendChild(imp);
        row.appendChild(mrg);
      } else {
        const note = document.createElement('span');
        note.className = 'muted';
        note.textContent = '(lesson pack — use the Go API/CLI)';
        row.appendChild(note);
      }
      listEl.appendChild(row);
    }
    log(`registry loaded: ${manifest.packs.length} pack(s) from ${url}`);
  } catch (e) {
    listEl.textContent = '✗ ' + e.message;
    log('✗ registry: ' + e.message);
  }
};

async function applyRegistryPack(manifestUrl, entry, mode) {
  try {
    if (mode === 'merge' && !selected) { log('select a creature first — it will absorb the pack lessons'); return; }
    const packJSON = await fetchPackVerified(manifestUrl, entry);
    const inspect = call('inspectPack', packJSON);
    if (!inspect) return;
    if (!confirmPack(entry, inspect)) { log(`skipped "${entry.name}"`); return; }
    if (mode === 'import') {
      const data = call('importPack', packJSON);
      if (!data) return;
      refresh(data.state);
      selected = null;
      log(`imported "${entry.name}" from the registry — ` + describeSig(data.pack_signature));
    } else {
      const data = call('mergeBrains', packJSON, selected);
      if (!data) return;
      refresh(data.state);
      log(`merged "${entry.name}" into creature #${selected} — ` + describeSig(data.pack_signature));
      syncBroadcastBrain();
    }
  } catch (e) {
    log('✗ registry pack: ' + e.message);
  }
}

// ---- live sync (P2P) ----
$('syncHost').onclick = async () => {
  if (!selected) { log('select a creature first — it is the one that will sync'); return; }
  try {
    $('syncBlob').value = await syncHost(selected);
    $('syncStatus').textContent = 'hosting — send the blob to your peer, paste their answer, press Accept answer';
    log('hosting a live-sync offer (copy the blob to your peer)');
  } catch (e) { log('✗ sync host: ' + e.message); }
};
$('syncJoin').onclick = async () => {
  if (!selected) { log('select a creature first — it is the one that will sync'); return; }
  if (!$('syncBlob').value.trim()) { log('paste the host\'s offer blob first'); return; }
  try {
    $('syncBlob').value = await syncJoin($('syncBlob').value, selected);
    $('syncStatus').textContent = 'joined — send this answer blob back to the host';
    log('answer created (copy the blob back to the host)');
  } catch (e) { log('✗ sync join: ' + e.message); }
};
$('syncAnswer').onclick = async () => {
  if (!$('syncBlob').value.trim()) { log('paste the joiner\'s answer blob first'); return; }
  try {
    await syncAcceptAnswer($('syncBlob').value);
    $('syncStatus').textContent = 'connecting…';
  } catch (e) { log('✗ sync accept: ' + e.message); }
};

$('syncAuto').onclick = async () => {
  if (!selected) { log('select a creature first — it is the one that will sync'); return; }
  const room = $('syncRoom').value.trim();
  if (!room) { log('enter a room code (share it with your peer)'); return; }
  $('syncStatus').textContent = `pairing in room "${room}"…`;
  try {
    const role = await syncAutoConnect($('syncSignalUrl').value.trim(), room, selected);
    log(`auto-connected as ${role} — the relay is closed; everything now flows peer-to-peer`);
  } catch (e) {
    $('syncStatus').textContent = 'offline';
    log('✗ auto connect: ' + e.message);
  }
};

// ---- identity (signing key) ----
function describeSig(sig) {
  if (!sig || !sig.signed) return 'pack was unsigned (replay-verifiable only)';
  return sig.valid ? `pack SIGNED by ${sig.fingerprint}` : 'pack signature INVALID';
}

$('exportKey').onclick = async () => {
  const backup = await exportIdentityBackup();
  if (!backup) { log('no identity to export'); return; }
  download('rulegarden-key.json', backup, 'application/json');
  log('key backup downloaded — anyone holding it can sign as you; store it privately');
};

$('importKey').onclick = () => $('keyFile').click();
$('keyFile').onchange = async () => {
  const f = $('keyFile').files[0];
  if (!f) return;
  const info = await importIdentityBackup(await f.text(), call);
  $('keyFile').value = '';
  if (!info) { log('✗ not a valid key backup'); return; }
  $('fp').textContent = info.fingerprint;
  log(`identity imported — now signing as ${info.fingerprint}`);
};

function b64ToBytes(b64) {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return bytes;
}

function download(name, content, mime) {
  const blob = new Blob([content], { type: mime });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  URL.revokeObjectURL(a.href);
}

// ---- boot ----
(async function boot() {
  const go = new Go();
  // Non-streaming instantiate: avoids wasm MIME-type issues on simple static servers.
  const bytes = await (await fetch('rulegarden.wasm')).arrayBuffer();
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
  go.run(instance); // resolves RuleGarden global, keeps runtime alive

  for (const id of ['tSees', 'xSees']) {
    const sel = $(id);
    clear(sel);
    for (const k of KINDS) sel.appendChild(el('option', { text: k })); // KINDS is a constant, but keep it DOM-only
  }
  refresh(call('newWorld', $('seed').value));

  // Live-sync wiring: the transport calls back into the same engine bridge + render path.
  liveSync.callEngine = call;
  liveSync.onApply = (data) => refresh(data);
  liveSync.onLog = log;
  liveSync.onState = (s) => { $('syncStatus').textContent = s; };

  // Identity boot: receipts and exported worlds are signed from here on. Unavailable key
  // storage (e.g. private browsing) degrades honestly to the old unsigned behavior.
  const id = await initIdentity(call);
  if (id) {
    $('fp').textContent = id.fingerprint;
    log(`signing identity ready: ${id.fingerprint} (key lives in this browser — see Export key)`);
  } else {
    $('fp').textContent = 'unsigned';
    log('no key storage available — receipts and exports stay unsigned (replay-verifiable only)');
  }
  log('RuleGarden ready. Place a creature and a predator, then teach.');
})();
