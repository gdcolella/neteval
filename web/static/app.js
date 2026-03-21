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
    function getDeployMode() {
        return document.getElementById('deploy-mode').value;
    }

    function updateDeployModeUI() {
        const mode = getDeployMode();
        document.getElementById('domain-fields').style.display = mode === 'domain' ? 'block' : 'none';
        document.getElementById('shared-cred-fields').style.display = mode === 'domain' ? 'block' : 'none';
    }

    function openDeployModal() {
        document.getElementById('deploy-modal').style.display = 'flex';
        updateDeployModeUI();

        // Load saved targets from server
        fetch('/api/deploy/machines')
            .then(r => r.json())
            .then(machines => {
                if (machines && machines.length > 0) {
                    // Show saved targets directly
                    discoveredMachines = machines;
                    renderMachineList();
                    document.getElementById('deploy-step-1').style.display = 'none';
                    document.getElementById('deploy-step-2').style.display = 'block';
                } else {
                    // No saved targets, show scan form
                    document.getElementById('deploy-step-1').style.display = 'block';
                    document.getElementById('deploy-step-2').style.display = 'none';
                }
            })
            .catch(() => {
                document.getElementById('deploy-step-1').style.display = 'block';
                document.getElementById('deploy-step-2').style.display = 'none';
            });
    }

    function closeDeployModal() {
        document.getElementById('deploy-modal').style.display = 'none';
    }

    function discoverMachines() {
        const mode = getDeployMode();
        const domain = document.getElementById('ad-domain').value;
        const username = document.getElementById('ad-username').value;
        const password = document.getElementById('ad-password').value;

        if (mode === 'domain' && (!domain || !username || !password)) {
            alert('Please fill in domain, username, and password');
            return;
        }

        const btn = document.getElementById('btn-discover');
        btn.disabled = true;
        btn.textContent = 'Scanning...';

        const body = mode === 'domain'
            ? { domain, username, password }
            : { domain: '', username: '', password: '' };

        fetch('/api/deploy/discover', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
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
            log('Found ' + machines.length + ' machines on the network');
        })
        .catch(err => {
            log('Scan error: ' + err.message);
            alert('Scan failed: ' + err.message);
        })
        .finally(() => {
            btn.disabled = false;
            btn.textContent = 'Scan Network';
        });
    }

    function manualAddIPs() {
        const text = document.getElementById('manual-ips').value.trim();
        if (!text) {
            alert('Enter at least one IP or hostname');
            return;
        }

        const ips = text.split('\n').map(s => s.trim()).filter(s => s);
        const btn = document.getElementById('btn-manual-add');
        btn.disabled = true;

        fetch('/api/deploy/manual', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ips })
        })
        .then(r => r.json())
        .then(machines => {
            discoveredMachines = discoveredMachines.concat(machines);
            renderMachineList();
            document.getElementById('deploy-step-1').style.display = 'none';
            document.getElementById('deploy-step-2').style.display = 'block';
            log('Added ' + machines.length + ' machines manually');
        })
        .catch(err => {
            log('Error: ' + err.message);
            alert('Failed: ' + err.message);
        })
        .finally(() => { btn.disabled = false; });
    }

    function renderMachineList() {
        const list = document.getElementById('machine-list');
        const mode = getDeployMode();
        document.getElementById('machine-count').textContent = discoveredMachines.length + ' machines found';

        if (mode === 'workgroup') {
            // Each machine gets its own username/password fields
            list.innerHTML = discoveredMachines.map((m, i) => `
                <div class="machine-item machine-item-workgroup">
                    <div class="machine-item-row">
                        <input type="checkbox" class="machine-check" data-index="${i}" checked>
                        <span class="machine-hostname">${esc(m.hostname)}</span>
                        <span class="machine-ip">${esc(m.ip)}</span>
                        <span class="machine-status ${m.status}">${esc(m.status)}</span>
                    </div>
                    <div class="machine-creds">
                        <input type="text" class="machine-user" data-index="${i}" placeholder="username" value="Administrator">
                        <input type="password" class="machine-pass" data-index="${i}" placeholder="password">
                    </div>
                </div>
            `).join('');
        } else {
            list.innerHTML = discoveredMachines.map((m, i) => `
                <div class="machine-item">
                    <input type="checkbox" class="machine-check" data-index="${i}" checked>
                    <span class="machine-hostname">${esc(m.hostname)}</span>
                    <span class="machine-ip">${esc(m.ip)}</span>
                    <span class="machine-status ${m.status}">${esc(m.status)}</span>
                </div>
            `).join('');
        }
    }

    function deploySelected() {
        const mode = getDeployMode();
        const checks = document.querySelectorAll('.machine-check:checked');

        if (checks.length === 0) {
            alert('No machines selected');
            return;
        }

        let body;

        if (mode === 'workgroup') {
            // Per-machine credentials
            const machines = [];
            checks.forEach(c => {
                const idx = c.dataset.index;
                const hostname = discoveredMachines[parseInt(idx)].hostname;
                const user = document.querySelector(`.machine-user[data-index="${idx}"]`).value;
                const pass = document.querySelector(`.machine-pass[data-index="${idx}"]`).value;
                if (!user || !pass) return;
                machines.push({
                    hostname: hostname,
                    credentials: { domain: '', username: user, password: pass }
                });
            });
            if (machines.length === 0) {
                alert('Please enter username and password for each selected machine');
                return;
            }
            body = { machines };
        } else {
            // Shared domain credentials
            const domain = document.getElementById('ad-domain').value;
            const username = document.getElementById('ad-username').value;
            const password = document.getElementById('ad-password').value;
            const hostnames = [];
            checks.forEach(c => {
                hostnames.push(discoveredMachines[parseInt(c.dataset.index)].hostname);
            });
            body = {
                credentials: { domain, username, password },
                hostnames: hostnames
            };
        }

        fetch('/api/deploy/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        })
        .then(r => r.json())
        .then(d => log('Deployment started'))
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
        document.getElementById('history-container').style.display = tab === 'history' ? '' : 'none';
        if (tab === 'history') loadHistory();
        else renderResults();
    }

    function loadHistory() {
        const container = document.getElementById('history-container');
        fetch('/api/history/runs')
            .then(r => r.json())
            .then(runs => {
                if (!runs || runs.length === 0) {
                    container.innerHTML = '<div class="no-results">No saved test runs yet</div>';
                    return;
                }
                let html = '<table class="internet-table"><thead><tr><th>Run</th><th>Type</th><th>Results</th><th>Avg Speed</th><th>Max</th><th>Time</th></tr></thead><tbody>';
                runs.forEach(run => {
                    const avg = formatSpeed(run.avg_bps);
                    const max = formatSpeed(run.max_bps);
                    const time = new Date(run.started_at).toLocaleString();
                    html += `<tr>
                        <td><code>${esc(run.run_id)}</code></td>
                        <td>${esc(run.test_type)}</td>
                        <td>${run.result_count}</td>
                        <td>${avg}</td>
                        <td>${max}</td>
                        <td>${time}</td>
                    </tr>`;
                });
                html += '</tbody></table>';
                container.innerHTML = html;
            })
            .catch(e => {
                container.innerHTML = '<div class="no-results">Failed to load history: ' + esc(e.message) + '</div>';
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
        document.getElementById('btn-manual-add').addEventListener('click', manualAddIPs);
        document.getElementById('btn-deploy-selected').addEventListener('click', deploySelected);
        document.getElementById('btn-rescan').addEventListener('click', () => {
            document.getElementById('deploy-step-1').style.display = 'block';
            document.getElementById('deploy-step-2').style.display = 'none';
        });
        document.getElementById('btn-add-more').addEventListener('click', () => {
            document.getElementById('deploy-step-1').style.display = 'block';
            document.getElementById('deploy-step-2').style.display = 'none';
        });
        document.getElementById('btn-export-csv').addEventListener('click', () => exportResults('csv'));
        document.getElementById('btn-export-json').addEventListener('click', () => exportResults('json'));
        document.getElementById('deploy-mode').addEventListener('change', updateDeployModeUI);

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

        // Auto-refresh agent list every 10s
        setInterval(() => {
            fetch('/api/agents')
                .then(r => r.json())
                .then(data => {
                    if (JSON.stringify(data) !== JSON.stringify(agents)) {
                        agents = data;
                        renderAgents();
                        renderResults();
                    }
                })
                .catch(() => {}); // silent on network errors
        }, 10000);
    });
})();
