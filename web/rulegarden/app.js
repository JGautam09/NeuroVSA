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

function basisSpan(basis) {
  return `<span class="basis-${basis}">${basis}</span>`;
}

function renderInspector() {
  const cr = (state.creatures || []).find((c) => c.id === selected);
  $('noSelection').style.display = cr ? 'none' : '';
  $('selection').style.display = cr ? '' : 'none';
  if (!cr) return;

  $('selId').textContent = '#' + cr.id;

  const d = cr.decision;
  if (d) {
    const cand = d.candidates.map((c) => `<tr><td>${c.token}</td><td>${c.distance}</td></tr>`).join('');
    const contrib = (d.contributors || [])
      .map((c) => `<span class="muted">↳ memory:</span> ${c.label || '#' + c.id}`).join('<br>');
    const p = d.percept.sees === 'nothing' ? 'nothing' : `${d.percept.sees}, ${d.percept.dist}, ${d.percept.dir}`;
    $('decision').innerHTML =
      `sees <b>${p}</b> → <b>${d.action}</b> (${basisSpan(d.basis)}, margin ${d.margin})` +
      `<table><tr><th>action</th><th>distance</th></tr>${cand}</table>` + contrib;
  } else {
    $('decision').innerHTML = '<span class="muted">no decision yet — tick the world</span>';
  }

  // lessons ledger
  const rows = (cr.lessons || []).map((l) =>
    `<tr class="lesson-row"><td>${l.id}</td><td class="${l.removed ? 'removed' : ''}">${l.label}</td>` +
    `<td>${l.removed ? '' : `<button data-forget="${l.id}">forget</button>`}</td></tr>`).join('');
  $('lessons').innerHTML = `<tr><th>id</th><th>lesson</th><th></th></tr>` + rows;
  for (const btn of $('lessons').querySelectorAll('button[data-forget]')) {
    btn.onclick = () => {
      refresh(call('apply', JSON.stringify({ op: 'forget', creature: selected, lesson: Number(btn.dataset.forget) })));
      log(`forgot lesson ${btn.dataset.forget} — memory is now bit-identical to never having learned it`);
    };
  }

  // transfer selects
  const active = (cr.lessons || []).filter((l) => !l.removed);
  $('xLesson').innerHTML = active.map((l) => `<option value="${l.id}">#${l.id} ${l.label.slice(0, 40)}</option>`).join('');
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
};

$('transfer').onclick = () => {
  if (!selected) return;
  const lesson = Number($('xLesson').value);
  refresh(call('apply', JSON.stringify({ op: 'transfer', creature: selected, lesson, new_sees: $('xSees').value })));
  log(`transferred lesson ${lesson} by analogy → subject ${$('xSees').value} (the dollar-of-Mexico move)`);
};

$('export').onclick = () => {
  const pack = call('exportPack');
  $('pack').value = JSON.stringify(pack);
  log(`exported world: seed + ${pack.events.length} events — that IS the whole world`);
};
$('import').onclick = () => {
  refresh(call('importPack', $('pack').value));
  selected = null;
  log('imported pack and replayed it deterministically');
};
$('hash').onclick = () => log('world hash: ' + call('hash'));

// ---- boot ----
(async function boot() {
  const go = new Go();
  // Non-streaming instantiate: avoids wasm MIME-type issues on simple static servers.
  const bytes = await (await fetch('rulegarden.wasm')).arrayBuffer();
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
  go.run(instance); // resolves RuleGarden global, keeps runtime alive

  for (const id of ['tSees', 'xSees']) {
    $(id).innerHTML = KINDS.map((k) => `<option>${k}</option>`).join('');
  }
  refresh(call('newWorld', $('seed').value));
  log('RuleGarden ready. Place a creature and a predator, then teach.');
})();
