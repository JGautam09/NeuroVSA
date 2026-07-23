import React, { useState, useEffect, useRef } from 'react';

export default function TerminalUI() {
  const [logs, setLogs] = useState([
    { id: 1, type: 'system', text: 'NeuroVSA (PhoneForge) v1.0.0 [Hyperdimensional Computing Engine]' },
    { id: 2, type: 'system', text: 'Vector Dim (D): 10,000 bits | Words: 157 uint64 | Zero-LLM Zero-PyTorch' },
    { id: 3, type: 'system', text: 'Connecting to Go WebSocket API at ws://localhost:8080/ws...' },
  ]);

  const [input, setInput] = useState('');
  const [status, setStatus] = useState('DISCONNECTED');
  const [stats, setStats] = useState({ dictSize: 0, latencyUs: 0 });

  const socketRef = useRef(null);
  const terminalEndRef = useRef(null);
  const tokenBufferRef = useRef([]);

  useEffect(() => {
    connectWebSocket();
    return () => {
      if (socketRef.current) socketRef.current.close();
    };
  }, []);

  useEffect(() => {
    terminalEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  const connectWebSocket = () => {
    const ws = new WebSocket('ws://localhost:8080/ws');

    ws.onopen = () => {
      setStatus('CONNECTED');
      addLog('system', '✓ Connected to NeuroVSA WebSocket Hub (ws://localhost:8080/ws)');
    };

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        handleServerMessage(data);
      } catch (err) {
        addLog('error', `Failed to parse socket message: ${event.data}`);
      }
    };

    ws.onclose = () => {
      setStatus('DISCONNECTED');
      addLog('system', '⚠ Disconnected from server. Reconnecting in 3s...');
      setTimeout(connectWebSocket, 3000);
    };

    ws.onerror = (err) => {
      setStatus('ERROR');
    };

    socketRef.current = ws;
  };

  const handleServerMessage = (data) => {
    if (data.type === 'token') {
      tokenBufferRef.current.push(data.value);
      setLogs((prev) => {
        const newLogs = [...prev];
        const lastIdx = newLogs.length - 1;
        if (lastIdx >= 0 && newLogs[lastIdx].type === 'stream') {
          newLogs[lastIdx] = {
            ...newLogs[lastIdx],
            text: newLogs[lastIdx].text + ' ' + data.value,
          };
        } else {
          newLogs.push({ id: Date.now(), type: 'stream', text: '➜ ' + data.value });
        }
        return newLogs;
      });
    } else if (data.type === 'trajectory') {
      addLog('trajectory', `[AGENT ROUTER] Tool: ${data.action} | Latency: ${data.latency_us}µs | ${data.summary}`);
      setStats((prev) => ({ ...prev, latencyUs: data.latency_us }));
    } else if (data.type === 'ast_done') {
      addLog('system', `[AST INDEXER] ${data.summary}`);
    } else if (data.type === 'done') {
      addLog('system', '✓ Sequence generation finished.');
    } else if (data.type === 'error') {
      addLog('error', `ERROR: ${data.error}`);
    }
  };

  const addLog = (type, text) => {
    setLogs((prev) => [...prev, { id: Date.now() + Math.random(), type, text }]);
  };

  const handleSubmit = (e) => {
    e.preventDefault();
    if (!input.trim() || !socketRef.current || socketRef.current.readyState !== WebSocket.OPEN) {
      return;
    }

    const command = input.trim();
    setInput('');
    addLog('user', `> ${command}`);

    if (command.startsWith('/route ')) {
      const goal = command.replace('/route ', '');
      socketRef.current.send(JSON.stringify({ type: 'route_tool', goal }));
    } else if (command.startsWith('/ast')) {
      const path = command.replace('/ast ', '').trim() || '.';
      socketRef.current.send(JSON.stringify({ type: 'index_ast', path }));
    } else {
      socketRef.current.send(JSON.stringify({ type: 'prompt', text: command }));
    }
  };

  return (
    <div className="min-h-screen bg-[#050B07] text-[#00ff66] flex flex-col p-4 font-mono">
      {/* Top Status Header */}
      <header className="border border-[#1A3A26] bg-[#0D1B12] p-3 rounded-t flex justify-between items-center mb-2">
        <div className="flex items-center space-x-3">
          <div className={`w-3 h-3 rounded-full ${status === 'CONNECTED' ? 'bg-[#00ff66] animate-pulse' : 'bg-red-500'}`}></div>
          <span className="font-bold text-lg tracking-wider">NEURO-VSA TERMINAL</span>
          <span className="text-xs text-[#00aa44] bg-[#002200] px-2 py-0.5 rounded">HYPERDIMENSIONAL ENGINE</span>
        </div>
        <div className="text-xs text-[#00aa44] flex space-x-4">
          <span>WS: <strong className="text-[#00ff66]">{status}</strong></span>
          <span>LATENCY: <strong className="text-[#00ff66]">{stats.latencyUs}µs</strong></span>
        </div>
      </header>

      {/* Main Terminal Window */}
      <div className="flex-1 border border-[#1A3A26] bg-[#050B07] p-4 rounded-b overflow-y-auto flex flex-col justify-between shadow-2xl">
        <div className="space-y-1 overflow-y-auto flex-1 pr-2">
          {logs.map((log) => (
            <div key={log.id} className="leading-relaxed text-sm">
              {log.type === 'system' && <span className="text-[#008833]">{log.text}</span>}
              {log.type === 'user' && <span className="text-white font-semibold">{log.text}</span>}
              {log.type === 'stream' && <span className="text-[#00ff66]">{log.text}</span>}
              {log.type === 'trajectory' && <span className="text-yellow-400 font-bold">{log.text}</span>}
              {log.type === 'error' && <span className="text-red-400">{log.text}</span>}
            </div>
          ))}
          <div ref={terminalEndRef} />
        </div>

        {/* Command Form */}
        <form onSubmit={handleSubmit} className="mt-4 border-t border-[#1A3A26] pt-3 flex items-center">
          <span className="text-[#00ff66] mr-2 font-bold">&gt;</span>
          <input
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="Type prompt (or /route <goal> or /ast <path>)..."
            className="flex-1 bg-transparent border-none outline-none text-[#00ff66] placeholder-[#004411] font-mono text-sm"
            autoFocus
          />
          <span className="w-2 h-5 bg-[#00ff66] cursor-blink inline-block ml-1"></span>
        </form>
      </div>

      {/* Helper Legend */}
      <footer className="mt-2 text-xs text-[#006622] flex justify-between">
        <span>Commands: <code>prompt</code> | <code>/route &lt;goal&gt;</code> | <code>/ast &lt;dir&gt;</code></span>
        <span>Math Core: XOR (⊗) | Permute (ρ) | Majority Vote (⊕)</span>
      </footer>
    </div>
  );
}
