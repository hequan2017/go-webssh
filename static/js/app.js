(() => {
  'use strict';

  const mode = document.querySelector('meta[name="go-webssh-mode"]').content;
  const loginView = document.querySelector('#login-view');
  const loginForm = document.querySelector('#login-form');
  const loginError = document.querySelector('#login-error');
  const appShell = document.querySelector('#app-shell');
  const navigation = document.querySelector('#navigation');
  const content = document.querySelector('#content');
  const pageTitle = document.querySelector('#page-title');
  const currentUser = document.querySelector('#current-user');
  const demoBadge = document.querySelector('#demo-badge');
  const modal = document.querySelector('#modal');
  const modalForm = document.querySelector('#modal-form');
  const modalTitle = document.querySelector('#modal-title');
  const modalBody = document.querySelector('#modal-body');
  const modalError = document.querySelector('#modal-error');
  const modalSubmitButton = modalForm.querySelector('button[type="submit"]');
  const toastRoot = document.querySelector('#toast');

  const state = {
    demo: mode === 'demo',
    me: null,
    assets: [],
    credentials: [],
    users: [],
    sessions: [],
    audits: [],
    terminal: null,
    socket: null,
    resizeHandler: null,
    playback: null,
    currentView: 'dashboard',
    modalSubmit: null
  };

  const titles = {
    dashboard: '概览', assets: '资产管理', credentials: '凭据管理', users: '用户与权限',
    sessions: '会话记录', audits: '审计日志', terminal: 'SSH 终端', files: '文件管理'
  };

  const escapeHTML = value => String(value ?? '').replace(/[&<>'"]/g, char => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;'
  }[char]));
  const formatTime = value => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  const formatSize = value => {
    const size = Number(value || 0);
    if (size < 1024) return `${size} B`;
    if (size < 1048576) return `${(size / 1024).toFixed(1)} KB`;
    if (size < 1073741824) return `${(size / 1048576).toFixed(1)} MB`;
    return `${(size / 1073741824).toFixed(1)} GB`;
  };
  const roleName = role => ({ admin: '管理员', operator: '运维人员', auditor: '审计员' }[role] || role);
  const credentialTypeName = type => type === 'private_key' ? 'SSH 私钥' : '密码';

  let toastTimer;
  const toast = (message, error = false) => {
    window.clearTimeout(toastTimer);
    toastRoot.textContent = message;
    toastRoot.className = `toast visible${error ? ' error' : ''}`;
    toastTimer = window.setTimeout(() => { toastRoot.className = 'toast'; }, 2600);
  };

  window.addEventListener('error', event => toast(`页面错误：${event.message}`, true));
  window.addEventListener('unhandledrejection', event => toast(`请求错误：${event.reason?.message || event.reason || '未知错误'}`, true));

  const api = async (url, options = {}) => {
    const response = await fetch(url, { credentials: 'same-origin', ...options });
    if (response.status === 204) return null;
    const type = response.headers.get('content-type') || '';
    const body = (type.includes('application/json') || type.includes('+json')) ? await response.json() : await response.text();
    if (!response.ok) {
      if (response.status === 401 && url !== '/api/auth/login') showLogin();
      throw new Error(body && body.error ? body.error : `请求失败 (${response.status})`);
    }
    return body;
  };

  const jsonOptions = (method, body) => ({
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  });

  const showLogin = () => {
    cleanupTerminal();
    state.me = null;
    appShell.hidden = true;
    loginView.hidden = false;
    document.querySelector('#login-username').focus();
  };

  const showApp = () => {
    loginView.hidden = true;
    appShell.hidden = false;
    currentUser.textContent = `${state.me.username} · ${roleName(state.me.role)}`;
    demoBadge.hidden = !state.demo;
    document.querySelectorAll('[data-roles]').forEach(item => {
      item.hidden = !item.dataset.roles.split(',').includes(state.me.role);
    });
  };

  const seedDemo = () => {
    const now = new Date().toISOString();
    state.me = { id: 'demo-admin', username: 'demo-admin', role: 'admin', enabled: true };
    state.credentials = [
      { id: 'cred-1', name: '生产环境运维密钥', type: 'private_key', created_at: now },
      { id: 'cred-2', name: '测试环境密码', type: 'password', created_at: now }
    ];
    state.assets = [
      { id: 'asset-1', name: '生产 Web 节点', host: '10.20.0.11', port: 22, username: 'ops', credential_id: 'cred-1', group: 'prod', description: 'Nginx 与 API 服务', enabled: true },
      { id: 'asset-2', name: '测试数据库', host: '10.30.0.21', port: 22, username: 'dba', credential_id: 'cred-2', group: 'test', description: 'MySQL 测试实例', enabled: true },
      { id: 'asset-3', name: '归档节点', host: '10.40.0.8', port: 2222, username: 'archive', credential_id: 'cred-1', group: 'archive', description: '离线归档', enabled: false }
    ];
    state.users = [
      { id: 'u1', username: 'admin', role: 'admin', asset_groups: ['*'], enabled: true, created_at: now },
      { id: 'u2', username: 'operator', role: 'operator', asset_groups: ['prod', 'test'], enabled: true, created_at: now },
      { id: 'u3', username: 'auditor', role: 'auditor', asset_groups: [], enabled: true, created_at: now }
    ];
    state.sessions = [{ id: 's1', username: 'operator', asset_name: '生产 Web 节点', client_ip: '192.168.1.20', status: 'closed', started_at: now, ended_at: now }];
    state.audits = [{ id: 'a1', time: now, username: 'operator', action: 'ssh.connect', resource_type: 'asset', resource_id: 'asset-1', client_ip: '192.168.1.20', success: true }];
  };

  const ensureData = async (...types) => {
    if (state.demo) return;
    await Promise.all(types.map(async type => {
      if (type === 'assets') state.assets = await api('/api/assets') || [];
      if (type === 'credentials') state.credentials = await api('/api/credentials') || [];
      if (type === 'users') state.users = await api('/api/users') || [];
      if (type === 'sessions') state.sessions = await api('/api/sessions') || [];
      if (type === 'audits') state.audits = await api('/api/audits?limit=300') || [];
    }));
  };

  const pageHead = (title, description, action = '') => `
    <div class="page-head"><div><h2>${escapeHTML(title)}</h2><p>${escapeHTML(description)}</p></div><div class="page-actions">${action}</div></div>`;

  const emptyRow = (columns, text) => `<tr><td colspan="${columns}" class="empty">${escapeHTML(text)}</td></tr>`;

  const renderDashboard = async () => {
    const data = ['assets'];
    if (['admin', 'auditor'].includes(state.me.role)) data.push('sessions', 'audits');
    await ensureData(...data);
    const active = state.sessions.filter(item => item.status === 'active').length;
    const failures = state.audits.filter(item => !item.success).length;
    const recent = state.audits.slice(0, 8);
    content.innerHTML = `
      ${pageHead('安全访问概览', '统一管理 SSH 资产、访问权限与审计证据。')}
      <section class="stats">
        <article class="stat-card"><span>受管资产</span><strong>${state.assets.length}</strong></article>
        <article class="stat-card"><span>可用资产</span><strong>${state.assets.filter(item => item.enabled).length}</strong></article>
        <article class="stat-card"><span>活动会话</span><strong>${active}</strong></article>
        <article class="stat-card"><span>近期失败事件</span><strong>${failures}</strong></article>
      </section>
      <section class="panel"><div class="panel-head"><h3>最近审计事件</h3></div><div class="table-wrap"><table>
        <thead><tr><th>时间</th><th>用户</th><th>动作</th><th>来源 IP</th><th>结果</th></tr></thead>
        <tbody>${recent.length ? recent.map(auditRow).join('') : emptyRow(5, state.me.role === 'operator' ? '当前角色不查看审计日志' : '暂无审计事件')}</tbody>
      </table></div></section>`;
  };

  const credentialName = id => state.credentials.find(item => item.id === id)?.name || '-';
  const assetActions = asset => {
    const connection = state.me.role !== 'auditor' ? `<button class="button button-primary" data-action="terminal" data-id="${asset.id}" ${asset.enabled ? '' : 'disabled'}>终端</button><button class="button" data-action="files" data-id="${asset.id}" ${asset.enabled ? '' : 'disabled'}>文件</button>` : '';
    const admin = state.me.role === 'admin' ? `<button class="button" data-action="test-asset" data-id="${asset.id}">测试</button><button class="button" data-action="edit-asset" data-id="${asset.id}">编辑</button><button class="button button-danger" data-action="delete-asset" data-id="${asset.id}">删除</button>` : '';
    return connection + admin;
  };

  const renderAssets = async () => {
    const types = ['assets'];
    if (state.me.role === 'admin') types.push('credentials');
    await ensureData(...types);
    const add = state.me.role === 'admin' ? '<button class="button button-primary" data-action="add-asset">新增资产</button>' : '';
    content.innerHTML = `${pageHead('资产管理', '通过资产组控制运维人员能够访问的服务器。', `<input id="asset-filter" class="filter-input" placeholder="搜索名称、地址、账号或资产组">${add}`)}
      <section class="panel"><div class="table-wrap"><table><thead><tr><th>名称</th><th>地址</th><th>账号</th><th>资产组</th><th>凭据</th><th>状态</th><th>操作</th></tr></thead>
      <tbody id="asset-rows">${state.assets.length ? state.assets.map(asset => `<tr data-search="${escapeHTML(`${asset.name} ${asset.host} ${asset.username} ${asset.group} ${asset.description || ''}`.toLowerCase())}"><td><strong>${escapeHTML(asset.name)}</strong><br><span class="muted">${escapeHTML(asset.description || '')}</span></td><td class="mono">${escapeHTML(asset.host)}:${asset.port}</td><td>${escapeHTML(asset.username)}</td><td><span class="badge">${escapeHTML(asset.group || 'default')}</span></td><td>${escapeHTML(credentialName(asset.credential_id))}</td><td><span class="badge ${asset.enabled ? 'badge-success' : 'badge-danger'}">${asset.enabled ? '可用' : '禁用'}</span></td><td><div class="table-actions">${assetActions(asset)}</div></td></tr>`).join('') : emptyRow(7, '暂无资产')}</tbody></table></div></section>`;
    document.querySelector('#asset-filter')?.addEventListener('input', event => {
      const query = event.target.value.trim().toLowerCase();
      document.querySelectorAll('#asset-rows tr[data-search]').forEach(row => { row.hidden = query !== '' && !row.dataset.search.includes(query); });
    });
  };

  const renderCredentials = async () => {
    await ensureData('credentials');
    content.innerHTML = `${pageHead('凭据管理', '密码和私钥使用 AES-GCM 加密保存，接口永不返回明文。', '<button class="button button-primary" data-action="add-credential">新增凭据</button>')}
      <section class="panel"><div class="table-wrap"><table><thead><tr><th>名称</th><th>类型</th><th>创建时间</th><th>操作</th></tr></thead><tbody>
      ${state.credentials.length ? state.credentials.map(item => `<tr><td>${escapeHTML(item.name)}</td><td><span class="badge">${credentialTypeName(item.type)}</span></td><td>${formatTime(item.created_at)}</td><td><div class="table-actions"><button class="button" data-action="edit-credential" data-id="${item.id}">编辑</button><button class="button button-danger" data-action="delete-credential" data-id="${item.id}">删除</button></div></td></tr>`).join('') : emptyRow(4, '暂无凭据')}</tbody></table></div></section>`;
  };

  const renderUsers = async () => {
    await ensureData('users');
    content.innerHTML = `${pageHead('用户与权限', '管理员管理全局资源；运维人员仅访问授权资产组；审计员只读审计数据。', '<button class="button button-primary" data-action="add-user">新增用户</button>')}
      <section class="panel"><div class="table-wrap"><table><thead><tr><th>用户名</th><th>角色</th><th>资产组</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead><tbody>
      ${state.users.length ? state.users.map(user => `<tr><td>${escapeHTML(user.username)}</td><td><span class="badge">${roleName(user.role)}</span></td><td>${escapeHTML((user.asset_groups || []).join(', ') || '-')}</td><td><span class="badge ${user.enabled ? 'badge-success' : 'badge-danger'}">${user.enabled ? '启用' : '禁用'}</span></td><td>${formatTime(user.created_at)}</td><td><button class="button" data-action="edit-user" data-id="${user.id}">编辑</button></td></tr>`).join('') : emptyRow(6, '暂无用户')}</tbody></table></div></section>`;
  };

  const auditRow = item => `<tr><td>${formatTime(item.time)}</td><td>${escapeHTML(item.username || '-')}</td><td class="mono">${escapeHTML(item.action)}</td><td>${escapeHTML(item.client_ip || '-')}</td><td><span class="badge ${item.success ? 'badge-success' : 'badge-danger'}">${item.success ? '成功' : '失败'}</span></td></tr>`;
  const renderAudits = async () => {
    await ensureData('audits');
    content.innerHTML = `${pageHead('审计日志', '登录、管理变更、SSH 命令和文件传输操作均写入追加式日志。', '<input id="audit-filter" class="filter-input" placeholder="搜索用户、动作、资源或 IP">')}
      <section class="panel"><div class="table-wrap"><table><thead><tr><th>时间</th><th>用户</th><th>动作</th><th>资源</th><th>来源 IP</th><th>结果</th></tr></thead><tbody id="audit-rows">${state.audits.length ? state.audits.map(item => `<tr data-search="${escapeHTML(`${item.username || ''} ${item.action} ${item.resource_type} ${item.resource_id || ''} ${item.client_ip || ''}`.toLowerCase())}"><td>${formatTime(item.time)}</td><td>${escapeHTML(item.username || '-')}</td><td class="mono">${escapeHTML(item.action)}</td><td>${escapeHTML(item.resource_type)}<br><span class="muted mono">${escapeHTML(item.resource_id || '-')}</span></td><td>${escapeHTML(item.client_ip || '-')}</td><td><span class="badge ${item.success ? 'badge-success' : 'badge-danger'}">${item.success ? '成功' : '失败'}</span></td></tr>`).join('') : emptyRow(6, '暂无审计事件')}</tbody></table></div></section>`;
    document.querySelector('#audit-filter')?.addEventListener('input', event => {
      const query = event.target.value.trim().toLowerCase();
      document.querySelectorAll('#audit-rows tr[data-search]').forEach(row => { row.hidden = query !== '' && !row.dataset.search.includes(query); });
    });
  };

  const renderSessions = async () => {
    await ensureData('sessions');
    content.innerHTML = `${pageHead('会话记录', '记录 SSH 会话参与者、资产、状态和输入输出录像。')}
      <section class="panel"><div class="table-wrap"><table><thead><tr><th>开始时间</th><th>用户</th><th>资产</th><th>来源 IP</th><th>状态</th><th>结束时间</th><th>录像</th></tr></thead><tbody>
      ${state.sessions.length ? state.sessions.map(item => `<tr><td>${formatTime(item.started_at)}</td><td>${escapeHTML(item.username)}</td><td>${escapeHTML(item.asset_name)}</td><td>${escapeHTML(item.client_ip)}</td><td><span class="badge ${item.status === 'active' ? 'badge-success' : item.status === 'failed' ? 'badge-danger' : ''}">${escapeHTML(item.status)}</span></td><td>${formatTime(item.ended_at)}</td><td><div class="table-actions">${state.demo ? '<button class="button" data-action="demo-only">回放</button>' : `<button class="button" data-action="play-recording" data-id="${item.id}">回放</button><a class="button" href="/api/sessions/${item.id}/recording">下载</a>`}${state.me.role === 'admin' && item.status === 'active' ? `<button class="button button-danger" data-action="terminate-session" data-id="${item.id}">强制断开</button>` : ''}</div></td></tr>`).join('') : emptyRow(7, '暂无会话')}</tbody></table></div></section>`;
  };

  const cleanupTerminal = () => {
    if (state.socket) {
      state.socket.close(1000, '切换页面');
      state.socket = null;
    }
    if (state.terminal) {
      state.terminal.destroy();
      state.terminal = null;
    }
    if (state.resizeHandler) {
      window.removeEventListener('resize', state.resizeHandler);
      state.resizeHandler = null;
    }
  };

  const openTerminal = asset => {
    cleanupTerminal();
    state.currentView = 'terminal';
    pageTitle.textContent = `SSH 终端 · ${asset.name}`;
    content.innerHTML = `<section class="terminal-page"><div class="terminal-toolbar"><div><button class="button" data-action="back-assets">← 返回资产</button><span id="terminal-status" class="badge">准备连接</span></div><div><button id="terminal-clear" class="button">清屏</button><button id="terminal-reconnect" class="button button-primary">重连</button><button id="terminal-fullscreen" class="button">全屏</button></div></div><div id="terminal-shell" class="terminal-shell"><div id="terminal"></div></div></section>`;
    const root = document.querySelector('#terminal');
    const shell = document.querySelector('#terminal-shell');
    const status = document.querySelector('#terminal-status');
    const reconnect = document.querySelector('#terminal-reconnect');
    const term = new Terminal({ cursorBlink: true, convertEol: true, fontSize: 14, fontFamily: 'Cascadia Code, Consolas, monospace', theme: { background: '#05070b', foreground: '#d8dee9', cursor: '#22c55e' } });
    state.terminal = term;
    term.open(root);
    const dimensions = () => ({ cols: Math.max(20, Math.floor((root.clientWidth - 24) / 8.4)), rows: Math.max(5, Math.floor((root.clientHeight - 24) / 17)) });
    const resize = () => { const size = dimensions(); term.resize(size.cols, size.rows); if (state.socket?.readyState === WebSocket.OPEN) state.socket.send(JSON.stringify({ type: 'resize', ...size })); return size; };
    state.resizeHandler = () => window.setTimeout(resize, 80);
    window.addEventListener('resize', state.resizeHandler);
    const connect = () => {
      const size = resize();
      reconnect.disabled = true;
      status.textContent = '正在连接';
      if (state.demo) {
        status.textContent = '静态演示';
        status.className = 'badge badge-info';
        reconnect.disabled = false;
        term.clear();
        term.writeln('\x1b[36mgo-webssh 跳板机终端演示\x1b[0m');
        term.writeln(`资产：${asset.name} (${asset.username}@${asset.host}:${asset.port})`);
        term.writeln('静态演示不会建立真实 SSH 连接。');
        term.write('\r\n\x1b[32mdemo@bastion\x1b[0m:$ ');
        return;
      }
      const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const socket = new WebSocket(`${protocol}//${location.host}/ws/${asset.id}?cols=${size.cols}&rows=${size.rows}`);
      state.socket = socket;
      socket.binaryType = 'arraybuffer';
      socket.onopen = () => { status.textContent = '已连接'; status.className = 'badge badge-success'; term.focus(); };
      socket.onmessage = event => term.write(event.data instanceof ArrayBuffer ? new Uint8Array(event.data) : event.data);
      socket.onerror = () => { status.textContent = '连接异常'; status.className = 'badge badge-danger'; };
      socket.onclose = event => { status.textContent = '已断开'; status.className = 'badge badge-danger'; reconnect.disabled = false; const reason = event.reason ? `：${event.reason}` : ''; term.writeln(`\r\n\x1b[31m连接已关闭${reason}\x1b[0m`); };
    };
    term.on('data', data => { if (state.socket?.readyState === WebSocket.OPEN) state.socket.send(data); else if (state.demo) term.write(data === '\r' ? '\r\n\x1b[32mdemo@bastion\x1b[0m:$ ' : data); });
    document.querySelector('#terminal-clear').onclick = () => { term.clear(); term.focus(); };
    reconnect.onclick = connect;
    document.querySelector('#terminal-fullscreen').onclick = async event => { if (document.fullscreenElement) await document.exitFullscreen(); else await shell.requestFullscreen(); event.currentTarget.textContent = document.fullscreenElement ? '退出全屏' : '全屏'; resize(); };
    connect();
  };

  const renderFiles = async (asset, remotePath = '.') => {
    cleanupTerminal();
    state.currentView = 'files';
    pageTitle.textContent = `文件管理 · ${asset.name}`;
    let result;
    if (state.demo) {
      result = { path: remotePath, files: [{ name: 'logs', path: 'logs', size: 0, mode: 'drwxr-xr-x', mod_time: new Date().toISOString(), is_dir: true }, { name: 'README.txt', path: 'README.txt', size: 2048, mode: '-rw-r--r--', mod_time: new Date().toISOString(), is_dir: false }] };
    } else {
      try { result = await api(`/api/assets/${asset.id}/files?path=${encodeURIComponent(remotePath)}`); } catch (error) { toast(error.message, true); go('assets'); return; }
    }
    const parent = result.path === '.' || result.path === '/' ? null : result.path.split('/').slice(0, -1).join('/') || '/';
    const pathParts = result.path === '.' ? [] : result.path.split('/').filter(Boolean);
    const breadcrumbs = [`<button class="button" data-action="open-dir" data-path="${result.path.startsWith('/') ? '/' : '.'}">根目录</button>`];
    pathParts.forEach((part, index) => {
      const prefix = result.path.startsWith('/') ? '/' : '';
      const targetPath = prefix + pathParts.slice(0, index + 1).join('/');
      breadcrumbs.push(`<span class="muted">/</span><button class="button" data-action="open-dir" data-path="${escapeHTML(targetPath)}">${escapeHTML(part)}</button>`);
    });
    content.innerHTML = `<div class="page-head"><div><h2>${escapeHTML(asset.name)} · 文件管理</h2><p class="mono file-path">${escapeHTML(result.path)}</p></div><button class="button" data-action="back-assets">返回资产</button></div>
      <section class="panel"><div class="panel-head file-toolbar"><div>${parent !== null ? `<button class="button" data-action="open-dir" data-path="${escapeHTML(parent)}">上级目录</button>` : ''}<button class="button" data-action="refresh-files" data-id="${asset.id}" data-path="${escapeHTML(result.path)}">刷新</button><button class="button" data-action="mkdir" data-path="${escapeHTML(result.path)}">新建目录</button></div><div><input id="upload-file" class="upload-input" type="file" multiple><button class="button button-primary" data-action="upload" data-id="${asset.id}" data-path="${escapeHTML(result.path)}">上传</button></div></div>
      <div class="panel-head"><div class="table-actions">${breadcrumbs.join('')}</div></div>
      <div class="table-wrap"><table><thead><tr><th>名称</th><th>大小</th><th>权限</th><th>修改时间</th><th>操作</th></tr></thead><tbody>${result.files.length ? result.files.map(file => `<tr><td>${file.is_dir ? '📁' : '📄'} ${escapeHTML(file.name)}</td><td>${file.is_dir ? '-' : formatSize(file.size)}</td><td class="mono">${escapeHTML(file.mode)}</td><td>${formatTime(file.mod_time)}</td><td><div class="table-actions">${file.is_dir ? `<button class="button" data-action="open-dir" data-path="${escapeHTML(file.path)}">打开</button>` : state.demo ? '<button class="button" data-action="demo-only">下载</button>' : `<a class="button" href="/api/assets/${asset.id}/download?path=${encodeURIComponent(file.path)}">下载</a>`}<button class="button" data-action="rename-file" data-path="${escapeHTML(file.path)}" data-name="${escapeHTML(file.name)}">重命名</button><button class="button button-danger" data-action="delete-file" data-path="${escapeHTML(file.path)}" data-name="${escapeHTML(file.name)}">删除</button></div></td></tr>`).join('') : emptyRow(5, '目录为空')}</tbody></table></div></section>`;
    content.dataset.assetId = asset.id;
    content.dataset.remotePath = result.path;
  };

  const go = async view => {
    if (state.currentView === 'terminal' && view !== 'terminal') cleanupTerminal();
    state.currentView = view;
    pageTitle.textContent = titles[view] || view;
    navigation.querySelectorAll('.nav-item').forEach(item => item.classList.toggle('active', item.dataset.view === view));
    appShell.classList.remove('menu-open');
    content.innerHTML = '<div class="empty">正在加载...</div>';
    try {
      if (view === 'dashboard') await renderDashboard();
      if (view === 'assets') await renderAssets();
      if (view === 'credentials') await renderCredentials();
      if (view === 'users') await renderUsers();
      if (view === 'sessions') await renderSessions();
      if (view === 'audits') await renderAudits();
      content.focus();
    } catch (error) {
      content.innerHTML = `<div class="empty">${escapeHTML(error.message)}</div>`;
    }
  };

  const openModal = (title, html, onSubmit, submitLabel = '保存') => {
	if (state.playback) { state.playback.destroy(); state.playback = null; }
    modalTitle.textContent = title;
    modalBody.innerHTML = html;
    modalError.textContent = '';
    state.modalSubmit = onSubmit;
    modalSubmitButton.textContent = submitLabel;
    modal.showModal();
    modalBody.querySelector('input, select, textarea')?.focus();
  };

  const mutable = () => {
    if (!state.demo) return true;
    toast('静态演示为只读，请本地运行服务端执行管理操作。', true);
    return false;
  };

  const credentialForm = item => `
    <div class="form-stack"><label>名称<input name="name" value="${escapeHTML(item?.name || '')}" required></label>
    <label>类型<select name="type"><option value="password" ${item?.type === 'password' ? 'selected' : ''}>密码</option><option value="private_key" ${item?.type === 'private_key' ? 'selected' : ''}>SSH 私钥</option></select></label>
    <label>凭据内容<textarea name="secret" placeholder="${item ? '留空表示不修改' : '输入密码或粘贴 PEM/OpenSSH 私钥；也可选择下方文件'}"></textarea></label>
    <label>从文件读取私钥（可选）<input name="secret_file" type="file" accept=".pem,.key"></label>
    <label>私钥口令（可选）<input name="passphrase" type="password" placeholder="仅加密私钥需要；留空表示不修改"></label></div>`;

  const openCredentialModal = item => openModal(item ? '编辑凭据' : '新增凭据', credentialForm(item), async form => {
    if (!mutable()) return false;
    const formData = new FormData(form);
    const secretFile = formData.get('secret_file');
    const data = { name: formData.get('name'), type: formData.get('type'), secret: formData.get('secret'), passphrase: formData.get('passphrase') };
    if (secretFile && secretFile.size > 0) data.secret = await secretFile.text();
    await api(item ? `/api/credentials/${item.id}` : '/api/credentials', jsonOptions(item ? 'PUT' : 'POST', data));
    toast('凭据已保存'); await go('credentials');
  });

  const assetForm = item => `<div class="form-grid">
    <label>资产名称<input name="name" value="${escapeHTML(item?.name || '')}" required></label><label>资产组<input name="group" value="${escapeHTML(item?.group || 'default')}" required></label>
    <label>主机/IP<input name="host" value="${escapeHTML(item?.host || '')}" required></label><label>SSH 端口<input name="port" type="number" min="1" max="65535" value="${item?.port || 22}" required></label>
    <label>SSH 用户名<input name="username" value="${escapeHTML(item?.username || 'root')}" required></label><label>登录凭据<select name="credential_id" required><option value="">请选择</option>${state.credentials.map(credential => `<option value="${credential.id}" ${credential.id === item?.credential_id ? 'selected' : ''}>${escapeHTML(credential.name)} · ${credentialTypeName(credential.type)}</option>`).join('')}</select></label>
    <label class="full">SSH 主机密钥指纹（推荐）<input name="host_key_fingerprint" value="${escapeHTML(item?.host_key_fingerprint || '')}" placeholder="SHA256:..."></label>
    <label class="full">描述<input name="description" value="${escapeHTML(item?.description || '')}"></label><label class="check-label full"><input name="enabled" type="checkbox" ${item ? item.enabled ? 'checked' : '' : 'checked'}>启用资产</label></div>`;

  const openAssetModal = item => openModal(item ? '编辑资产' : '新增资产', assetForm(item), async form => {
    if (!mutable()) return false;
    const raw = Object.fromEntries(new FormData(form));
    const data = { ...raw, port: Number(raw.port), enabled: form.elements.enabled.checked };
    await api(item ? `/api/assets/${item.id}` : '/api/assets', jsonOptions(item ? 'PUT' : 'POST', data));
    toast('资产已保存'); await go('assets');
  });

  const userForm = item => `<div class="form-grid">
    ${item ? '' : `<label class="full">用户名<input name="username" required></label>`}
    <label>角色<select name="role"><option value="admin" ${item?.role === 'admin' ? 'selected' : ''}>管理员</option><option value="operator" ${item?.role === 'operator' ? 'selected' : ''}>运维人员</option><option value="auditor" ${item?.role === 'auditor' ? 'selected' : ''}>审计员</option></select></label>
    <label>资产组<input name="asset_groups" value="${escapeHTML((item?.asset_groups || []).join(','))}" placeholder="prod,test 或 *"></label>
    <label class="full">${item ? '新密码（留空不修改）' : '密码'}<input name="password" type="password" minlength="12" ${item ? '' : 'required'}></label>
    ${item ? `<label class="check-label full"><input name="enabled" type="checkbox" ${item.enabled ? 'checked' : ''}>启用用户</label>` : ''}</div>`;

  const openUserModal = item => openModal(item ? '编辑用户' : '新增用户', userForm(item), async form => {
    if (!mutable()) return false;
    const raw = Object.fromEntries(new FormData(form));
    const data = { ...raw, asset_groups: raw.asset_groups.split(',').map(value => value.trim()).filter(Boolean) };
    if (item) data.enabled = form.elements.enabled.checked;
    await api(item ? `/api/users/${item.id}` : '/api/users', jsonOptions(item ? 'PUT' : 'POST', data));
    toast('用户已保存'); await go('users');
  });

  navigation.addEventListener('click', event => {
    const item = event.target.closest('[data-view]');
    if (item) go(item.dataset.view);
  });

  content.addEventListener('click', async event => {
    const target = event.target.closest('[data-action]');
    if (!target) return;
    const action = target.dataset.action;
    const asset = state.assets.find(item => item.id === target.dataset.id) || state.assets.find(item => item.id === content.dataset.assetId);
    try {
      if (action === 'add-asset') openAssetModal(null);
      if (action === 'edit-asset') openAssetModal(asset);
      if (action === 'test-asset') {
        if (!mutable()) return;
        target.disabled = true; target.textContent = '测试中...';
        try {
          const result = await api(`/api/assets/${asset.id}/test`, { method: 'POST' });
          toast(`SSH 认证成功，耗时 ${result.latency_ms} ms`);
        } finally {
          target.disabled = false; target.textContent = '测试';
        }
      }
      if (action === 'delete-asset' && mutable() && window.confirm(`确认删除资产“${asset.name}”？`)) { await api(`/api/assets/${asset.id}`, { method: 'DELETE' }); toast('资产已删除'); await go('assets'); }
      if (action === 'terminal') openTerminal(asset);
      if (action === 'files') await renderFiles(asset);
      if (action === 'back-assets') await go('assets');
      if (action === 'add-credential') openCredentialModal(null);
      if (action === 'edit-credential') openCredentialModal(state.credentials.find(item => item.id === target.dataset.id));
      if (action === 'delete-credential' && mutable() && window.confirm('确认删除该凭据？')) { await api(`/api/credentials/${target.dataset.id}`, { method: 'DELETE' }); toast('凭据已删除'); await go('credentials'); }
      if (action === 'add-user') openUserModal(null);
      if (action === 'edit-user') openUserModal(state.users.find(item => item.id === target.dataset.id));
      if (action === 'open-dir') await renderFiles(asset, target.dataset.path);
      if (action === 'refresh-files') await renderFiles(asset, target.dataset.path);
      if (action === 'upload') {
        if (!mutable()) return;
        const files = Array.from(document.querySelector('#upload-file').files);
        if (!files.length) throw new Error('请先选择文件');
        target.disabled = true;
        try {
          for (let index = 0; index < files.length; index++) {
            target.textContent = `上传 ${index + 1}/${files.length}`;
            const body = new FormData(); body.append('file', files[index]);
            await api(`/api/assets/${asset.id}/files?path=${encodeURIComponent(target.dataset.path)}`, { method: 'POST', body });
          }
          toast(`${files.length} 个文件上传成功`); await renderFiles(asset, target.dataset.path);
        } finally {
          target.disabled = false; target.textContent = '上传';
        }
      }
      if (action === 'mkdir') openModal('新建目录', '<div class="form-stack"><label>目录名称<input name="name" required></label></div>', async form => {
        if (!mutable()) return false;
        const name = new FormData(form).get('name');
        await api(`/api/assets/${asset.id}/directories`, jsonOptions('POST', { path: target.dataset.path, name }));
        toast('目录已创建'); await renderFiles(asset, target.dataset.path);
      });
      if (action === 'rename-file') openModal('重命名', `<div class="form-stack"><label>新名称<input name="name" value="${escapeHTML(target.dataset.name)}" required></label></div>`, async form => {
        if (!mutable()) return false;
        const newName = new FormData(form).get('name');
        await api(`/api/assets/${asset.id}/files`, jsonOptions('PATCH', { path: target.dataset.path, new_name: newName }));
        toast('重命名成功'); await renderFiles(asset, content.dataset.remotePath);
      });
      if (action === 'delete-file' && mutable() && window.confirm(`确认删除“${target.dataset.name}”？目录只能在为空时删除。`)) {
        await api(`/api/assets/${asset.id}/files?path=${encodeURIComponent(target.dataset.path)}`, { method: 'DELETE' });
        toast('删除成功'); await renderFiles(asset, content.dataset.remotePath);
      }
      if (action === 'demo-only') toast('静态演示不会下载或修改文件。', true);
      if (action === 'terminate-session' && mutable() && window.confirm('确认强制断开该活动 SSH 会话？')) {
        await api(`/api/sessions/${target.dataset.id}`, { method: 'DELETE' });
        toast('会话已发送断开指令'); await go('sessions');
      }
      if (action === 'play-recording') {
        const recording = await api(`/api/sessions/${target.dataset.id}/recording`);
        openModal('终端会话回放', '<div id="recording-terminal" class="recording-shell"></div>', async () => {}, '关闭');
        const playback = new Terminal({ cursorBlink: false, disableStdin: true, convertEol: true, fontSize: 13, fontFamily: 'Cascadia Code, Consolas, monospace', theme: { background: '#05070b', foreground: '#d8dee9' } });
        state.playback = playback;
        playback.open(document.querySelector('#recording-terminal'));
        recording.split('\n').filter(Boolean).forEach(line => {
          try {
            const frame = JSON.parse(line);
            if (frame.direction !== 'output') return;
            const binary = window.atob(frame.data);
            const bytes = Uint8Array.from(binary, char => char.charCodeAt(0));
            playback.write(new TextDecoder().decode(bytes));
          } catch (_) {}
        });
      }
    } catch (error) { toast(error.message, true); }
  });

  modalForm.addEventListener('submit', async event => {
    event.preventDefault();
    modalError.textContent = '';
    try {
      const result = await state.modalSubmit(modalForm);
      if (result !== false) closeModal();
    } catch (error) { modalError.textContent = error.message; }
  });
  const closeModal = () => { modal.close(); if (state.playback) { state.playback.destroy(); state.playback = null; } };
  document.querySelector('#modal-close').onclick = closeModal;
  document.querySelector('#modal-cancel').onclick = closeModal;
  modal.addEventListener('close', () => { if (state.playback) { state.playback.destroy(); state.playback = null; } });
  document.querySelector('#menu-toggle').onclick = () => appShell.classList.toggle('menu-open');
  document.querySelector('#change-password').onclick = () => openModal('修改登录密码', '<div class="form-stack"><label>当前密码<input name="current_password" type="password" required></label><label>新密码<input name="new_password" type="password" minlength="12" required></label><label>确认新密码<input name="confirm_password" type="password" minlength="12" required></label></div>', async form => {
    if (!mutable()) return false;
    const values = Object.fromEntries(new FormData(form));
    if (values.new_password !== values.confirm_password) throw new Error('两次输入的新密码不一致');
    await api('/api/me/password', jsonOptions('POST', { current_password: values.current_password, new_password: values.new_password }));
    toast('密码已修改，请重新登录'); showLogin();
  });
  document.querySelector('#logout').onclick = async () => {
    if (state.demo) { toast('静态演示不创建登录会话。'); return; }
    try { await api('/api/auth/logout', { method: 'POST' }); } catch (_) {}
    showLogin();
  };

  loginForm.addEventListener('submit', async event => {
    event.preventDefault();
    loginError.textContent = '';
    try {
      const body = Object.fromEntries(new FormData(loginForm));
      state.me = await api('/api/auth/login', jsonOptions('POST', body));
      loginForm.reset(); showApp(); await go('dashboard');
    } catch (error) { loginError.textContent = error.message; }
  });

  const boot = async () => {
    if (state.demo) {
      seedDemo(); showApp(); await go('dashboard'); return;
    }
    try { state.me = await api('/api/me'); showApp(); await go('dashboard'); } catch (_) { showLogin(); }
  };

  boot();
})();
