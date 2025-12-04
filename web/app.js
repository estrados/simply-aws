const app = document.getElementById('app');
const regionSelect = document.getElementById('region-select');

let allRegions = [];
let currentRegion = '';

async function api(path, opts) {
  const res = await fetch(path, opts);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

async function init() {
  const [status, regions] = await Promise.all([
    api('/api/status'),
    api('/api/regions').catch(() => [])
  ]);

  allRegions = regions || [];
  currentRegion = status?.aws?.region || '';
  populateDropdown();
  app.innerHTML = '';
}

function populateDropdown() {
  regionSelect.innerHTML = '';
  const enabled = allRegions.filter(r => r.enabled);
  if (enabled.length === 0) {
    const opt = document.createElement('option');
    opt.textContent = currentRegion || 'No regions';
    regionSelect.appendChild(opt);
  } else {
    for (const r of enabled) {
      const opt = document.createElement('option');
      opt.value = r.name;
      opt.textContent = r.name;
      if (r.name === currentRegion) opt.selected = true;
      regionSelect.appendChild(opt);
    }
  }
}

// --- Settings page ---

function openSettings() {
  let html = `<div class="settings-overlay" onclick="if(event.target===this)closeSettings()">
    <div class="settings-panel">
      <div class="settings-header">
        <h2>Settings</h2>
        <button class="settings-close" onclick="closeSettings()">&times;</button>
      </div>
      <div class="settings-body">
        <h3>Regions</h3>
        <p class="settings-desc">Select which regions appear in the dropdown.</p>
        <div class="region-actions">
          <button class="btn btn-sm" onclick="toggleAllRegions(true)">Enable All</button>
          <button class="btn btn-sm btn-outline" onclick="toggleAllRegions(false)">Disable All</button>
        </div>
        <div class="region-list" id="region-list">`;

  for (const r of allRegions) {
    html += `<label class="region-item">
      <input type="checkbox" ${r.enabled ? 'checked' : ''} data-region="${r.name}" onchange="toggleRegion('${r.name}', this.checked)">
      <span>${r.name}</span>
    </label>`;
  }

  html += `</div></div></div></div>`;
  document.getElementById('settings-container').innerHTML = html;
}

function closeSettings() {
  document.getElementById('settings-container').innerHTML = '';
}

async function toggleRegion(name, enabled) {
  await api(`/api/regions/${name}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled })
  });
  const r = allRegions.find(r => r.name === name);
  if (r) r.enabled = enabled;
  populateDropdown();
}

async function toggleAllRegions(enabled) {
  const promises = allRegions.map(r =>
    api(`/api/regions/${r.name}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled })
    })
  );
  await Promise.all(promises);
  allRegions.forEach(r => r.enabled = enabled);
  populateDropdown();

  // Update checkboxes
  document.querySelectorAll('#region-list input[type="checkbox"]').forEach(cb => {
    cb.checked = enabled;
  });
}

init();
