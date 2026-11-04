// MarkToast web UI — vanilla, no framework (see docs/TECH-STACK.md).
// It talks to the local hub's /api/status endpoint; memory browsing arrives
// with the index work in M1/M2.

const $ = (id) => document.getElementById(id);

async function loadStatus() {
  const status = $('hub-status');
  try {
    const res = await fetch('/api/status');
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = await res.json();
    status.textContent = `hub :${data.hub} · ${data.facts} 条记忆`;
    status.classList.add('ok');

    $('version').textContent = data.version ?? '—';
    $('fact-count').textContent = String(data.facts ?? 0);
    $('root').textContent = data.root ?? '—';

    renderList('projects', data.projects ?? []);
    renderList('adapters', data.adapters ?? []);
  } catch (err) {
    status.textContent = 'hub 未连接';
    status.classList.add('err');
    console.error(err);
  }
}

function renderList(id, items) {
  const list = $(id);
  list.innerHTML = '';
  if (!items.length) {
    const li = document.createElement('li');
    li.textContent = '（空）';
    list.appendChild(li);
    return;
  }
  for (const item of items) {
    const li = document.createElement('li');
    li.textContent = item;
    li.title = item;
    list.appendChild(li);
  }
}

loadStatus();
setInterval(loadStatus, 10000);
