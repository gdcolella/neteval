(function() {
    'use strict';

    let ws = null;
    let agents = [];
    let results = [];
    let testRunning = false;
    let activeTab = 'mesh';
    let discoveredMachines = [];

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
                    renderResults();
                }
                break;

            case 'agent_list':
                agents = msg.payload.agents || [];
                renderAgents();
                renderResults();
                break;

            case 'test_result':
                results.push(msg.payload);
                renderResults();
                log('Result: ' + formatResult(msg.payload));
                break;

            case 'test_progress':
                updateProgress(msg.payload);
                break;

            case 'tests_complete':
                testRunning = false;
                updateButtons();
                hideProgress();
                log('All tests complete');
                break;

            case 'deploy_status':
                handleDeployStatus(msg.payload);
                break;
        }
    }

    // --- Agents ---
    function renderAgents() {
        const list = document.getElementById('agent-list');
        const count = document.getElementById('agent-count');
        count.textContent = agents.length;

        if (agents.length === 0) {
            list.innerHTML = '<li class="no-results">No agents connected</li>';
        } else {
            list.innerHTML = agents.map(a => `
                <li>
                    <span class="status-dot connected"></span>
                    <span class="agent-name">${esc(a.hostname)}</span>
                    <span class="agent-detail">${esc(a.ip)} &middot; port ${a.speed_port} &middot; ${esc(a.os)}</span>
                </li>
            `).join('');
        }
        updateButtons();
    }

    // --- Results rendering ---
    function renderResults() {
        if (activeTab === 'mesh') renderHeatmap();
        else renderInternetResults();
    }

    function renderHeatmap() {
        const container = document.getElementById('mesh-container');
        const meshResults = results.filter(r => r.test_type === 'mesh');

        if (agents.length < 2 || meshResults.length === 0) {
            container.innerHTML = '<div class="no-results">Run a mesh test to see results</div>';
            return;
        }

        const matrix = {};
        meshResults.forEach(r => {
            if (!matrix[r.source_id]) matrix[r.source_id] = {};
            if (!matrix[r.source_id][r.target_id]) matrix[r.source_id][r.target_id] = {};
            matrix[r.source_id][r.target_id][r.direction] = r;
        });

        let html = '<table class="heatmap"><thead><tr><th></th>';
        agents.forEach(a => { html += `<th>${esc(a.hostname)}</th>`; });
        html += '</tr></thead><tbody>';

        agents.forEach(src => {
            html += `<tr><td class="row-header">${esc(src.hostname)}</td>`;
            agents.forEach(dst => {
                if (src.id === dst.id) {
                    html += '<td class="result self">&mdash;</td>';
                    return;
                }

                const data = matrix[src.id] && matrix[src.id][dst.id];
                if (!data) {
                    html += '<td class="result">&hellip;</td>';
                    return;
                }

                const ul = data.upload;
                const dl = data.download;
                let text = '';
                let cls = '';

                if (ul && !ul.error) {
                    text += '&uarr;' + formatSpeed(ul.bits_per_sec);
                    cls = speedClass(ul.bits_per_sec);
                }
                if (dl && !dl.error) {
                    if (text) text += ' ';
                    text += '&darr;' + formatSpeed(dl.bits_per_sec);
                    if (!cls) cls = speedClass(dl.bits_per_sec);
                }
                if ((ul && ul.error) || (dl && dl.error)) { text = text || 'Error'; cls = cls || 'speed-error'; }
                if (!text) { text = '&hellip;'; cls = ''; }

                html += `<td class="result ${cls}" title="${esc(buildTooltip(src, dst, data))}">${text}</td>`;
            });
            html += '</tr>';
        });

        html += '</tbody></table>';
        container.innerHTML = html;
    }

    function renderInternetResults() {
        const container = document.getElementById('internet-container');
        const inetResults = results.filter(r => r.test_type === 'internet');

        if (inetResults.length === 0) {
            container.innerHTML = '<div class="no-results">Run an internet test to see each agent\'s WAN speed</div>';
            return;
        }

        // Group by source
        const byAgent = {};
        inetResults.forEach(r => {
            if (!byAgent[r.source_id]) byAgent[r.source_id] = { name: r.source_name || r.source_id };
            byAgent[r.source_id][r.direction] = r;
        });

        // Find max for bar scaling
        let maxBps = 0;
        Object.values(byAgent).forEach(a => {
            if (a.download && a.download.bits_per_sec > maxBps) maxBps = a.download.bits_per_sec;
            if (a.upload && a.upload.bits_per_sec > maxBps) maxBps = a.upload.bits_per_sec;
        });
        if (maxBps === 0) maxBps = 1;

        let html = '<table class="internet-table"><thead><tr><th>Agent</th><th>Download</th><th>Upload</th></tr></thead><tbody>';

        Object.values(byAgent).forEach(a => {
            const dlBps = (a.download && !a.download.error) ? a.download.bits_per_sec : 0;
            const ulBps = (a.upload && !a.upload.error) ? a.upload.bits_per_sec : 0;
            const dlPct = (dlBps / maxBps * 100).toFixed(1);
            const ulPct = (ulBps / maxBps * 100).toFixed(1);
            const dlErr = a.download && a.download.error;
            const ulErr = a.upload && a.upload.error;

            html += `<tr>
                <td><strong>${esc(a.name)}</strong></td>
                <td><div class="speed-bar-container">
                    <div class="speed-bar download" style="width:${dlPct}%"></div>
                    <span>${dlErr ? esc(dlErr) : formatSpeed(dlBps)}</span>
                </div></td>
                <td><div class="speed-bar-container">
                    <div class="speed-bar upload" style="width:${ulPct}%"></div>
                    <span>${ulErr ? esc(ulErr) : formatSpeed(ulBps)}</span>
                </div></td>
            </tr>`;
        });

        html += '</tbody></table>';
        container.innerHTML = html;
    }

    function buildTooltip(src, dst, data) {
        let tip = src.hostname + ' -> ' + dst.hostname + '\n';
        if (data.upload && !data.upload.error) tip += 'Upload: ' + formatSpeed(data.upload.bits_per_sec) + '\n';
        if (data.download && !data.download.error) tip += 'Download: ' + formatSpeed(data.download.bits_per_sec) + '\n';
        return tip;
    }

    // --- Progress ---
    function showProgress(text) {
        const bar = document.getElementById('progress-bar');
        bar.style.display = 'block';
        document.getElementById('progress-text').textContent = text;
        document.getElementById('progress-fill').style.width = '0%';
    }

    function updateProgress(payload) {
        const bar = document.getElementById('progress-bar');
        bar.style.display = 'block';
        const src = payload.source_id;
        const dst = payload.target_id || 'internet';
        const pct = payload.percent || 0;
        document.getElementById('progress-text').textContent =
            `Testing ${src} -> ${dst} (${payload.direction}) - ${formatSpeed(payload.bits_per_sec)}`;
        document.getElementById('progress-fill').style.width = pct + '%';
    }

    function hideProgress() {
        document.getElementById('progress-bar').style.display = 'none';
    }

    // --- Deploy modal ---
    function openDeployModal() {
        document.getElementById('deploy-modal').style.display = 'flex';
        document.getElementById('deploy-step-1').style.display = 'block';
        document.getElementById('deploy-step-2').style.display = 'none';
    }

    function closeDeployModal() {
        document.getElementById('deploy-modal').style.display = 'none';
    }

    function discoverMachines() {
        const domain = document.getElementById('ad-domain').value;
        const username = document.getElementById('ad-username').value;
        const password = document.getElementById('ad-password').value;

        if (!domain || !username || !password) {
            alert('Please fill in all fields');
            return;
        }

        const btn = document.getElementById('btn-discover');
        btn.disabled = true;
        btn.textContent = 'Discovering...';

        fetch('/api/deploy/discover', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ domain, username, password })
        })
        .then(r => {
            if (!r.ok) return r.text().then(t => { throw new Error(t); });
            return r.json();
        })
        .then(machines => {
            discoveredMachines = machines;
            renderMachineList();
            document.getElementById('deploy-step-1').style.display = 'none';
            document.getElementById('deploy-step-2').style.display = 'block';
            log('Discovered ' + machines.length + ' machines');
        })
        .catch(err => {
            log('Discovery error: ' + err.message);
            alert('Discovery failed: ' + err.message);
        })
        .finally(() => {
            btn.disabled = false;
            btn.textContent = 'Discover Machines';
        });
    }

    function renderMachineList() {
        const list = document.getElementById('machine-list');
        document.getElementById('machine-count').textContent = discoveredMachines.length + ' machines found';

        list.innerHTML = discoveredMachines.map((m, i) => `
            <div class="machine-item">
                <input type="checkbox" class="machine-check" data-index="${i}" checked>
                <span class="machine-hostname">${esc(m.hostname)}</span>
                <span class="machine-ip">${esc(m.ip)}</span>
                <span class="machine-status ${m.status}">${esc(m.status)}</span>
            </div>
        `).join('');
    }

    function deploySelected() {
        const checks = document.querySelectorAll('.machine-check:checked');
        const hostnames = [];
        checks.forEach(c => {
            const idx = parseInt(c.dataset.index);
            hostnames.push(discoveredMachines[idx].hostname);
        });

        if (hostnames.length === 0) {
            alert('No machines selected');
            return;
        }

        const domain = document.getElementById('ad-domain').value;
        const username = document.getElementById('ad-username').value;
        const password = document.getElementById('ad-password').value;

        fetch('/api/deploy/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                credentials: { domain, username, password },
                hostnames: hostnames
            })
        })
        .then(r => r.json())
        .then(d => log('Deployment started for ' + hostnames.length + ' machines'))
        .catch(err => log('Deploy error: ' + err.message));
    }

    function handleDeployStatus(payload) {
        log('Deploy ' + (payload.hostname || '') + ': ' + payload.status +
            (payload.error ? ' - ' + payload.error : ''));

        // Update machine list if modal is open
        if (payload.hostname) {
            discoveredMachines.forEach(m => {
                if (m.hostname === payload.hostname) {
                    m.status = payload.status;
                    if (payload.error) m.error = payload.error;
                }
            });
            if (document.getElementById('deploy-modal').style.display !== 'none') {
                renderMachineList();
            }
        }
    }

    // --- Formatting helpers ---
    function formatSpeed(bps) {
        if (bps >= 1e9) return (bps / 1e9).toFixed(1) + ' Gbps';
        if (bps >= 1e6) return (bps / 1e6).toFixed(1) + ' Mbps';
        if (bps >= 1e3) return (bps / 1e3).toFixed(0) + ' Kbps';
        return bps.toFixed(0) + ' bps';
    }

    function speedClass(bps) {
        if (bps >= 900e6) return 'speed-excellent';
        if (bps >= 100e6) return 'speed-good';
        if (bps >= 10e6) return 'speed-fair';
        return 'speed-poor';
    }

    function formatResult(r) {
        if (r.error) return (r.source_name || r.source_id) + ': error - ' + r.error;
        const dir = r.direction === 'upload' ? '\u2191' : '\u2193';
        const target = r.target_name || r.target_id || 'internet';
        return (r.source_name || r.source_id) + ' \u2192 ' + target + ' ' + dir + ' ' + formatSpeed(r.bits_per_sec);
    }

    function updateConnectionStatus(connected) {
        const dot = document.getElementById('conn-status');
        dot.className = 'status-dot ' + (connected ? 'connected' : 'disconnected');
    }

    function updateButtons() {
        document.getElementById('btn-mesh').disabled = testRunning || agents.length < 2;
        document.getElementById('btn-internet').disabled = testRunning || agents.length < 1;
    }

    // --- Actions ---
    function runMeshTest() {
        testRunning = true;
        updateButtons();
        showProgress('Starting mesh speed test...');
        log('Starting mesh speed test...');
        fetch('/api/tests/mesh', { method: 'POST' })
            .then(r => r.json())
            .then(d => log('Mesh test: ' + d.status))
            .catch(e => { log('Error: ' + e.message); testRunning = false; updateButtons(); hideProgress(); });
    }

    function runInternetTest() {
        testRunning = true;
        updateButtons();
        showProgress('Starting internet speed test...');
        log('Starting internet speed test...');
        fetch('/api/tests/internet', { method: 'POST' })
            .then(r => r.json())
            .then(d => log('Internet test: ' + d.status))
            .catch(e => { log('Error: ' + e.message); testRunning = false; updateButtons(); hideProgress(); });
    }

    function clearResults() {
        fetch('/api/results/clear', { method: 'POST' }).then(() => {
            results = [];
            renderResults();
            log('Results cleared');
        });
    }

    function exportResults(format) {
        window.open('/api/results/export?format=' + format, '_blank');
    }

    function switchTab(tab) {
        activeTab = tab;
        document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.tab === tab));
        document.getElementById('mesh-container').style.display = tab === 'mesh' ? '' : 'none';
        document.getElementById('internet-container').style.display = tab === 'internet' ? '' : 'none';
        renderResults();
    }

    function log(msg) {
        const entries = document.getElementById('log-entries');
        const time = new Date().toLocaleTimeString();
        const entry = document.createElement('div');
        entry.className = 'log-entry';
        entry.innerHTML = '<span class="timestamp">[' + time + ']</span> ' + esc(msg);
        entries.appendChild(entry);
        entries.scrollTop = entries.scrollHeight;
        while (entries.children.length > 200) entries.removeChild(entries.firstChild);
    }

    function esc(s) {
        if (!s) return '';
        const d = document.createElement('div');
        d.textContent = s;
        return d.innerHTML;
    }

    // --- Init ---
    document.addEventListener('DOMContentLoaded', () => {
        document.getElementById('btn-mesh').addEventListener('click', runMeshTest);
        document.getElementById('btn-internet').addEventListener('click', runInternetTest);
        document.getElementById('btn-clear').addEventListener('click', clearResults);
        document.getElementById('btn-deploy').addEventListener('click', openDeployModal);
        document.getElementById('modal-close').addEventListener('click', closeDeployModal);
        document.getElementById('btn-discover').addEventListener('click', discoverMachines);
        document.getElementById('btn-deploy-selected').addEventListener('click', deploySelected);
        document.getElementById('btn-export-csv').addEventListener('click', () => exportResults('csv'));
        document.getElementById('btn-export-json').addEventListener('click', () => exportResults('json'));

        document.getElementById('select-all').addEventListener('change', (e) => {
            document.querySelectorAll('.machine-check').forEach(c => c.checked = e.target.checked);
        });

        document.querySelectorAll('.tab').forEach(t => {
            t.addEventListener('click', () => switchTab(t.dataset.tab));
        });

        // Close modal on backdrop click
        document.getElementById('deploy-modal').addEventListener('click', (e) => {
            if (e.target === e.currentTarget) closeDeployModal();
        });

        updateButtons();
        connect();
    });
})();
