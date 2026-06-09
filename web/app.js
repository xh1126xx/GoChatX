// ── State ──
let ws = null;
let token = null;
let selfId = null;
let currentRoom = null;
let currentPrivate = null;
let joinedRooms = new Set();
let reconnectAttempts = 0;
const MAX_RECONNECT_ATTEMPTS = 10;
let reconnectTimer = null;
let pingTimer = null;

// ── DOM refs ──
const $authView = document.getElementById('auth-view');
const $chatView = document.getElementById('chat-view');
const $authMsg = document.getElementById('auth-msg');
const $loginForm = document.getElementById('login-form');
const $registerForm = document.getElementById('register-form');
const $selfId = document.getElementById('self-id');
const $roomList = document.getElementById('room-list');
const $userList = document.getElementById('user-list');
const $onlineCount = document.getElementById('online-count');
const $chatHeader = document.getElementById('chat-header');
const $chatHeaderText = document.getElementById('chat-header-text');
const $chatMessages = document.getElementById('chat-messages');
const $msgInput = document.getElementById('msg-input');
const $sendBtn = document.getElementById('send-btn');
const $roomInput = document.getElementById('room-input');
const $logoutBtn = document.getElementById('logout-btn');
const $devHint = document.querySelector('.dev-hint');

// ── Auth ──
document.querySelectorAll('.auth-tab').forEach(tab => {
  tab.onclick = () => {
    document.querySelectorAll('.auth-tab').forEach(t => t.classList.remove('active'));
    tab.classList.add('active');
    const which = tab.dataset.tab;
    $loginForm.style.display = which === 'login' ? '' : 'none';
    $registerForm.style.display = which === 'register' ? '' : 'none';
    $authMsg.textContent = '';
  };
});

$loginForm.onsubmit = async (e) => {
  e.preventDefault();
  const fd = new FormData($loginForm);
  const username = fd.get('username').trim();
  const password = fd.get('password').trim();
  if (!username) return;

  try {
    const res = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
    const data = await res.json();
    if (data.ok && data.token) {
      token = data.token;
    } else {
      if (data.msg && data.msg.includes('unavailable')) {
        token = null;
      } else {
        $authMsg.textContent = data.msg || 'login failed';
        return;
      }
    }
  } catch {
    token = null;
  }

  if (!token) {
    $devHint.style.display = '';
  }

  connectWS(username);
};

$registerForm.onsubmit = async (e) => {
  e.preventDefault();
  const fd = new FormData($registerForm);
  const username = fd.get('username').trim();
  const password = fd.get('password').trim();
  if (!username || !password) return;

  try {
    const res = await fetch('/api/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
    const data = await res.json();
    if (data.ok) {
      $authMsg.style.color = '#22c55e';
      $authMsg.textContent = '注册成功，请切换到登录';
      document.querySelector('.auth-tab[data-tab="login"]').click();
    } else {
      $authMsg.textContent = data.msg || 'register failed';
    }
  } catch {
    $authMsg.textContent = '服务器不可用';
  }
};

// ── WebSocket ──
function connectWS(username) {
  // Close existing connection before creating a new one
  if (ws) {
    ws.onclose = null;
    ws.close();
    ws = null;
  }

  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  ws = new WebSocket(`${proto}://${location.host}/ws`);

  // Connection timeout (10s)
  const connectTimeout = setTimeout(() => {
    if (ws && ws.readyState === WebSocket.CONNECTING) {
      ws.close();
    }
  }, 10000);

  ws.onopen = () => {
    clearTimeout(connectTimeout);
    reconnectAttempts = 0;
    const authToken = token || username;
    ws.send(JSON.stringify({ type: 'auth', token: authToken }));
  };

  ws.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data);
      handleMessage(msg);
    } catch { /* ignore malformed */ }
  };

  ws.onclose = (e) => {
    clearTimeout(connectTimeout);
    if (!selfId) return;

    // Don't reconnect on auth failure (code 4001) or normal closure
    if (e.code === 4001 || e.code === 1000) return;

    reconnectAttempts++;
    if (reconnectAttempts > MAX_RECONNECT_ATTEMPTS) {
      addSystemMsg('重连次数过多，请手动刷新页面');
      return;
    }

    // Exponential backoff: 1s, 2s, 4s, 8s, 16s, 30s max
    const delay = Math.min(1000 * Math.pow(2, reconnectAttempts - 1), 30000);
    addSystemMsg(`连接断开，${Math.round(delay / 1000)}秒后自动重连 (${reconnectAttempts}/${MAX_RECONNECT_ATTEMPTS})...`);
    reconnectTimer = setTimeout(() => {
      if (!selfId) return;
      connectWS(username);
    }, delay);
  };

  ws.onerror = () => {
    // onerror is followed by onclose, which handles reconnection
  };
}

function send(msg) {
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  ws.send(JSON.stringify(msg));
}

// ── Message Handler ──
function handleMessage(msg) {
  switch (msg.type) {
    case 'authed':
      selfId = msg.data.user;
      $selfId.textContent = selfId;
      $authView.style.display = 'none';
      $chatView.style.display = 'flex';
      startPing();
      refreshOnlineUsers();
      $msgInput.focus();
      break;

    case 'joined':
      currentRoom = msg.data.room;
      currentPrivate = null;
      joinedRooms.add(currentRoom);
      renderRooms();
      $chatHeaderText.textContent = '#' + currentRoom;
      $chatMessages.innerHTML = '';
      addSystemMsg('已加入 ' + currentRoom);
      if (msg.data.history && msg.data.history.length) {
        msg.data.history.reverse().forEach(m => addChatMsg(m.from, m.content, m.timestamp, m.from === selfId));
      }
      scrollBottom();
      highlightActive();
      break;

    case 'left':
      joinedRooms.delete(msg.data);
      if (currentRoom === msg.data) {
        currentRoom = null;
        $chatHeaderText.textContent = '欢迎来到 GoChatX';
      }
      renderRooms();
      addSystemMsg('已离开 ' + msg.data);
      break;

    case 'message':
      addChatMsg(msg.from || msg.data?.from, msg.msg || msg.data?.msg, msg.ts || msg.data?.ts, (msg.from || msg.data?.from) === selfId);
      scrollBottom();
      break;

    case 'private':
      addChatMsg(msg.from || msg.data?.from, msg.msg || msg.data?.msg, msg.ts || msg.data?.ts, (msg.from || msg.data?.from) === selfId, true);
      scrollBottom();
      break;

    case 'history':
      if (msg.data && msg.data.length) {
        msg.data.reverse().forEach(m => addChatMsg(m.from, m.content, m.timestamp, m.from === selfId));
        scrollBottom();
      }
      break;

    case 'ack':
      break;

    case 'pong':
      break;

    case 'error':
      addSystemMsg('错误: ' + (msg.data || 'unknown'));
      break;
  }
}

// ── Send Message ──
function sendMessage() {
  const content = $msgInput.value.trim();
  if (!content) return;

  if (currentPrivate) {
    send({ type: 'private', to: currentPrivate, content });
    addChatMsg(selfId, content, Date.now(), true, true);
    scrollBottom();
  } else if (currentRoom) {
    send({ type: 'send', room_id: currentRoom, content });
  } else {
    addSystemMsg('请先加入房间或选择私聊用户');
    return;
  }
  $msgInput.value = '';
}

$sendBtn.onclick = sendMessage;
$msgInput.onkeydown = (e) => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    sendMessage();
  }
};

// ── File Upload ──
const $uploadBtn = document.getElementById('upload-btn');
const $fileInput = document.getElementById('file-input');

$uploadBtn.onclick = () => $fileInput.click();
$fileInput.onchange = async () => {
  const file = $fileInput.files[0];
  if (!file) return;
  $fileInput.value = '';

  if (file.size > 10 * 1024 * 1024) {
    addSystemMsg('文件过大，最大 10MB');
    return;
  }

  const formData = new FormData();
  formData.append('file', file);

  try {
    addSystemMsg('上传中...');
    const res = await fetch('/api/upload', {
      method: 'POST',
      headers: token ? { 'Authorization': 'Bearer ' + token } : {},
      body: formData,
    });
    const data = await res.json();
    if (data.ok) {
      // Send file message
      const isImage = /\.(jpg|jpeg|png|gif|webp)$/i.test(data.url);
      const content = isImage ? '[图片] ' + data.url : '[文件] ' + data.filename + ' ' + data.url;
      if (currentPrivate) {
        send({ type: 'private', to: currentPrivate, content });
        addChatMsg(selfId, content, Date.now(), true, true);
      } else if (currentRoom) {
        send({ type: 'send', room_id: currentRoom, content });
      }
      scrollBottom();
    } else {
      addSystemMsg('上传失败: ' + (data.msg || 'unknown'));
    }
  } catch {
    addSystemMsg('上传失败: 网络错误');
  }
};

// ── Room Management ──
$roomInput.onkeydown = (e) => {
  if (e.key === 'Enter') {
    const room = $roomInput.value.trim();
    if (room) {
      send({ type: 'join', room_id: room });
      $roomInput.value = '';
    }
  }
};

function renderRooms() {
  $roomList.innerHTML = '';
  joinedRooms.forEach(room => {
    const div = document.createElement('div');
    div.className = 'room-item' + (room === currentRoom && !currentPrivate ? ' active' : '');
    // Use textContent for room name to prevent XSS
    const dot = document.createElement('span');
    dot.className = 'dot';
    const nameSpan = document.createElement('span');
    nameSpan.textContent = room;
    const leaveBtn = document.createElement('span');
    leaveBtn.className = 'leave-x';
    leaveBtn.textContent = '×';
    div.appendChild(dot);
    div.appendChild(nameSpan);
    div.appendChild(leaveBtn);
    div.onclick = (e) => {
      if (e.target.classList.contains('leave-x')) {
        send({ type: 'leave', room_id: room });
      } else {
        switchRoom(room);
      }
    };
    $roomList.appendChild(div);
  });
}

function switchRoom(room) {
  if (currentRoom === room && !currentPrivate) return;
  currentRoom = room;
  currentPrivate = null;
  $chatHeaderText.textContent = '#' + room;
  $chatMessages.innerHTML = '';
  send({ type: 'history', room_id: room, limit: 50 });
  highlightActive();
}

// ── Online Users ──
function refreshOnlineUsers() {
  fetch('/api/users/online')
    .then(r => r.json())
    .then(data => {
      if (data.ok) {
        renderUsers(data.users || []);
      }
    })
    .catch(() => {
      renderUsers([]);
    });
}

function renderUsers(users) {
  $userList.innerHTML = '';
  $onlineCount.textContent = users.length;
  const others = users.filter(u => u !== selfId);
  others.forEach(user => {
    const div = document.createElement('div');
    div.className = 'user-item' + (user === currentPrivate ? ' active' : '');
    // Use textContent for username to prevent XSS
    const dot = document.createElement('span');
    dot.className = 'dot';
    const nameSpan = document.createElement('span');
    nameSpan.textContent = user;
    div.appendChild(dot);
    div.appendChild(nameSpan);
    div.onclick = () => startPrivate(user);
    $userList.appendChild(div);
  });
}

function startPrivate(user) {
  currentPrivate = user;
  currentRoom = null;
  $chatHeaderText.textContent = '@' + user + ' (私聊)';
  $chatMessages.innerHTML = '';
  addSystemMsg('私聊中 — 消息不会广播到房间');
  highlightActive();
}

// ── Message Display ──
function addChatMsg(from, content, ts, isSelf, isPrivate) {
  const div = document.createElement('div');
  div.className = 'msg-row ' + (isSelf ? 'self' : 'other');

  const time = ts ? new Date(ts).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) : '';

  // Escape both `from` and `content` to prevent XSS
  const rendered = renderContent(content);
  div.innerHTML =
    '<div class="msg-meta">' + (isPrivate ? '@' : '') + escapeHtml(from) + (time ? ' ' + time : '') + '</div>' +
    '<div class="msg-bubble">' + rendered + '</div>';

  $chatMessages.appendChild(div);

  // Limit DOM nodes to 500 messages
  while ($chatMessages.children.length > 500) {
    $chatMessages.removeChild($chatMessages.firstChild);
  }
}

function addSystemMsg(text) {
  const div = document.createElement('div');
  div.className = 'msg-system';
  div.textContent = text;
  $chatMessages.appendChild(div);
}

function scrollBottom() {
  requestAnimationFrame(() => {
    $chatMessages.scrollTop = $chatMessages.scrollHeight;
  });
}

function highlightActive() {
  document.querySelectorAll('.room-item').forEach(el => {
    const nameEl = el.querySelector('span:not(.dot):not(.leave-x)');
    const name = nameEl ? nameEl.textContent : '';
    el.classList.toggle('active', name === (currentPrivate ? '' : currentRoom));
  });
  document.querySelectorAll('.user-item').forEach(el => {
    const nameEl = el.querySelector('span:not(.dot)');
    const name = nameEl ? nameEl.textContent : '';
    el.classList.toggle('active', name === currentPrivate);
  });
}

function escapeHtml(s) {
  if (s == null) return '';
  const d = document.createElement('div');
  d.textContent = String(s);
  return d.innerHTML;
}

// Render message content: images inline, file links clickable
function renderContent(content) {
  if (!content) return '';
  // Image pattern: [图片] /uploads/xxx.jpg
  const imgMatch = content.match(/\[图片\]\s*(\/uploads\/[\w.-]+\.(jpg|jpeg|png|gif|webp))/i);
  if (imgMatch) {
    return '<img src="' + escapeHtml(imgMatch[1]) + '" style="max-width:300px;max-height:200px;border-radius:8px;cursor:pointer" onclick="window.open(this.src)" alt="图片">';
  }
  // File pattern: [文件] filename /uploads/xxx.ext
  const fileMatch = content.match(/\[文件\]\s*(.+?)\s*(\/uploads\/[\w.-]+)/);
  if (fileMatch) {
    return '📄 <a href="' + escapeHtml(fileMatch[2]) + '" target="_blank" style="color:inherit;text-decoration:underline">' + escapeHtml(fileMatch[1]) + '</a>';
  }
  // URL auto-link
  const urlRegex = /(https?:\/\/[^\s<]+)/g;
  const escaped = escapeHtml(content);
  return escaped.replace(urlRegex, '<a href="$1" target="_blank" rel="noopener" style="color:inherit;text-decoration:underline">$1</a>');
}

// ── Heartbeat ──
function startPing() {
  if (pingTimer) clearInterval(pingTimer);
  pingTimer = setInterval(() => {
    send({ type: 'ping' });
    refreshOnlineUsers();
  }, 15000);
}

// ── Textarea auto-resize ──
$msgInput.addEventListener('input', () => {
  $msgInput.style.height = 'auto';
  $msgInput.style.height = Math.min($msgInput.scrollHeight, 120) + 'px';
});

// ── Mobile sidebar toggle ──
const $menuBtn = document.getElementById('menu-btn');
const $sidebar = document.querySelector('.sidebar');
const $overlay = document.getElementById('sidebar-overlay');

if ($menuBtn) {
  $menuBtn.onclick = () => {
    $sidebar.classList.toggle('open');
    $overlay.classList.toggle('open');
  };
}
if ($overlay) {
  $overlay.onclick = () => {
    $sidebar.classList.remove('open');
    $overlay.classList.remove('open');
  };
}

// ── Logout ──
$logoutBtn.onclick = () => {
  if (reconnectTimer) clearTimeout(reconnectTimer);
  reconnectAttempts = 0;
  if (ws) {
    ws.onclose = null;
    ws.close();
  }
  ws = null;
  selfId = null;
  currentRoom = null;
  currentPrivate = null;
  joinedRooms.clear();
  token = null;
  if (pingTimer) clearInterval(pingTimer);
  $authView.style.display = '';
  $chatView.style.display = 'none';
  $chatMessages.innerHTML = '<div class="empty-hint">选择一个房间加入，或点击在线用户开始私聊</div>';
  $authMsg.textContent = '';
};
