(function() {
    'use strict';

    let ws = null;
    let agents = [];
    let results = [];
    let testRunning = false;

    function connect() {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(proto + '//' + location.host + '/ws/dashboard');

        ws.onopen = () => {
            log('Connected to coordinator');
            updateConnectionStatus(true);
        };

        ws.onclose = () => {
            log('Disconnected from coordinator');
            updateConnectionStatus(false);
            setTimeout(connect, 2000);
        };

        ws.onerror = () => {};

        ws.onmessage = (event) => {
            const msg = JSON.parse(event.data);
            handleMessage(msg);
        };
    }

    function handleMessage(msg) {
        switch (msg.type) {
            case 'dashboard_update':
                if (msg.payload.agents) {
                    agents = msg.payload.agents;
                    renderAgents();
                }
                if (msg.payload.results && msg.payload.results.length > 0) {
                    results = msg.payload.results;
                    renderHeatmap();
                }
                break;

            case 'agent_list':
                agents = msg.payload.agents || [];
                renderAgents();
                renderHeatmap();
                break;

            case 'test_result':
                results.push(msg.payload);
                renderHeatmap();
                log('Result: ' + formatResult(msg.payload));
                break;

            case 'test_progress':
                log('Testing: ' + msg.payload.source_id + ' → ' +
                    (msg.payload.target_id || 'internet') + ' ' +
                    formatSpeed(msg.payload.bits_per_sec));
                break;

            case 'tests_complete':
                testRunning = false;
                updateButtons();
                log('All tests complete');
                break;
        }
    }

    function renderAgents() {
        const list = document.getElementById('agent-list');
        const count = document.getElementById('agent-count');
        count.textContent = agents.length;

        if (agents.length === 0) {
            list.innerHTML = '<li class="no-results">No agents connected</li>';
            return;
        }

        list.innerHTML = agents.map(a => `
            <li>
                <span class="status-dot connected"></span>
                <span class="agent-name">${esc(a.hostname)}</span>
                <span class="agent-detail">${esc(a.ip)} · port ${a.speed_port} · ${esc(a.os)}</span>
            </li>
        `).join('');

        updateButtons();
    }

    function renderHeatmap() {
        const container = document.getElementById('heatmap-container');

        if (agents.length < 2 || results.length === 0) {
            container.innerHTML = '<div class="no-results">Run a mesh test to see results</div>';
            return;
        }

        // Build a lookup: results[sourceID][targetID][direction] = bps
        const matrix = {};
        results.forEach(r => {
            if (r.test_type !== 'mesh') return;
            if (!matrix[r.source_id]) matrix[r.source_id] = {};
            if (!matrix[r.source_id][r.target_id]) matrix[r.source_id][r.target_id] = {};
            matrix[r.source_id][r.target_id][r.direction] = r;
        });

        // Build table
        let html = '<table class="heatmap"><thead><tr><th></th>';
        agents.forEach(a => {
            html += `<th>${esc(a.hostname)}</th>`;
        });
        html += '</tr></thead><tbody>';

        agents.forEach(src => {
            html += `<tr><td class="row-header">${esc(src.hostname)}</td>`;
            agents.forEach(dst => {
                if (src.id === dst.id) {
                    html += '<td class="result self">—</td>';
                    return;
                }

                const data = matrix[src.id] && matrix[src.id][dst.id];
                if (!data) {
                    html += '<td class="result">…</td>';
                    return;
                }

                const ul = data.upload;
                const dl = data.download;
                let text = '';
                let cls = '';

                if (ul && !ul.error) {
                    text += '↑' + formatSpeed(ul.bits_per_sec);
                    cls = speedClass(ul.bits_per_sec);
                }
                if (dl && !dl.error) {
                    if (text) text += ' ';
                    text += '↓' + formatSpeed(dl.bits_per_sec);
                    if (!cls) cls = speedClass(dl.bits_per_sec);
                }

                if (ul && ul.error) { text = 'Error'; cls = 'speed-error'; }
                if (!text) { text = '…'; cls = ''; }

                html += `<td class="result ${cls}" title="${esc(buildTooltip(src, dst, data))}">${text}</td>`;
            });
            html += '</tr>';
        });

        html += '</tbody></table>';
        container.innerHTML = html;
    }

    function buildTooltip(src, dst, data) {
        let tip = src.hostname + ' → ' + dst.hostname + '\n';
        if (data.upload && !data.upload.error) {
            tip += 'Upload: ' + formatSpeed(data.upload.bits_per_sec) + '\n';
        }
        if (data.download && !data.download.error) {
            tip += 'Download: ' + formatSpeed(data.download.bits_per_sec) + '\n';
        }
        return tip;
    }

    function formatSpeed(bps) {
        if (bps >= 1e9) return (bps / 1e9).toFixed(1) + ' Gbps';
        if (bps >= 1e6) return (bps / 1e6).toFixed(1) + ' Mbps';
        if (bps >= 1e3) return (bps / 1e3).toFixed(0) + ' Kbps';
        return bps.toFixed(0) + ' bps';
    }

    function speedClass(bps) {
        if (bps >= 900e6) return 'speed-excellent';  // 900+ Mbps
        if (bps >= 100e6) return 'speed-good';       // 100+ Mbps
        if (bps >= 10e6) return 'speed-fair';        // 10+ Mbps
        return 'speed-poor';
    }

    function formatResult(r) {
        if (r.error) return r.source_name + ': error - ' + r.error;
        const dir = r.direction === 'upload' ? '↑' : '↓';
        const target = r.target_name || 'internet';
        return r.source_name + ' → ' + target + ' ' + dir + ' ' + formatSpeed(r.bits_per_sec);
    }

    function updateConnectionStatus(connected) {
        const dot = document.getElementById('conn-status');
        dot.className = 'status-dot ' + (connected ? 'connected' : 'disconnected');
    }

    function updateButtons() {
        const meshBtn = document.getElementById('btn-mesh');
        const internetBtn = document.getElementById('btn-internet');
        meshBtn.disabled = testRunning || agents.length < 2;
        internetBtn.disabled = testRunning || agents.length < 1;
    }

    function runMeshTest() {
        testRunning = true;
        updateButtons();
        log('Starting mesh speed test...');
        fetch('/api/tests/mesh', { method: 'POST' })
            .then(r => r.json())
            .then(d => log('Mesh test: ' + d.status))
            .catch(e => {
                log('Error: ' + e.message);
                testRunning = false;
                updateButtons();
            });
    }

    function runInternetTest() {
        testRunning = true;
        updateButtons();
        log('Starting internet speed test...');
        fetch('/api/tests/internet', { method: 'POST' })
            .then(r => r.json())
            .then(d => log('Internet test: ' + d.status))
            .catch(e => {
                log('Error: ' + e.message);
                testRunning = false;
                updateButtons();
            });
    }

    function clearResults() {
        fetch('/api/results/clear', { method: 'POST' })
            .then(() => {
                results = [];
                renderHeatmap();
                log('Results cleared');
            });
    }

    function log(msg) {
        const entries = document.getElementById('log-entries');
        const time = new Date().toLocaleTimeString();
        const entry = document.createElement('div');
        entry.className = 'log-entry';
        entry.innerHTML = '<span class="timestamp">[' + time + ']</span> ' + esc(msg);
        entries.appendChild(entry);
        entries.scrollTop = entries.scrollHeight;

        // Keep max 100 entries
        while (entries.children.length > 100) {
            entries.removeChild(entries.firstChild);
        }
    }

    function esc(s) {
        if (!s) return '';
        const d = document.createElement('div');
        d.textContent = s;
        return d.innerHTML;
    }

    // Initialize
    document.addEventListener('DOMContentLoaded', () => {
        document.getElementById('btn-mesh').addEventListener('click', runMeshTest);
        document.getElementById('btn-internet').addEventListener('click', runInternetTest);
        document.getElementById('btn-clear').addEventListener('click', clearResults);
        updateButtons();
        connect();
    });
})();
