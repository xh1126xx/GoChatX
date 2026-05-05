// ── State ──
let ws = null;
let token = null;
let selfId = null;
let currentRoom = null;
let currentPrivate = null; // user ID for private chat
let joinedRooms = new Set();

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

  // Try REST API first
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
      // fallback: dev mode (no auth service)
      if (data.msg && data.msg.includes('unavailable')) {
        token = null;
      } else {
        $authMsg.textContent = data.msg || 'login failed';
        return;
      }
    }
  } catch (err) {
    // Server not reachable or no REST endpoint; try dev mode
    token = null;
  }

  // If no token, show dev hint and use username as token
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
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  ws = new WebSocket(`${proto}://${location.host}/ws`);

  ws.onopen = () => {
    const authToken = token || username;
    ws.send(JSON.stringify({ type: 'auth', token: authToken }));
  };

  ws.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data);
      handleMessage(msg);
    } catch { /* ignore malformed */ }
  };

  ws.onclose = () => {
    if (selfId) {
      addSystemMsg('连接断开，3秒后自动重连...');
      setTimeout(() => { if (!selfId) return; connectWS(username); }, 3000);
    }
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
      $chatHeader.textContent = '#' + currentRoom;
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
        $chatHeader.textContent = '欢迎来到 GoChatX';
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
      // private message sent confirmation
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
    div.innerHTML = '<span class="dot"></span>' + room + '<span class="leave-x">×</span>';
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
  $chatHeader.textContent = '#' + room;
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
      // fallback: empty list
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
    div.innerHTML = '<span class="dot"></span>' + user;
    div.onclick = () => startPrivate(user);
    $userList.appendChild(div);
  });
}

function startPrivate(user) {
  currentPrivate = user;
  currentRoom = null;
  $chatHeader.textContent = '@' + user + ' (私聊)';
  $chatMessages.innerHTML = '';
  addSystemMsg('私聊中 — 消息不会广播到房间');
  highlightActive();
}

// ── Message Display ──
function addChatMsg(from, content, ts, isSelf, isPrivate) {
  const div = document.createElement('div');
  div.className = 'msg-row ' + (isSelf ? 'self' : 'other');

  const time = ts ? new Date(ts).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) : '';

  div.innerHTML =
    '<div class="msg-meta">' + (isPrivate ? '@' : '') + from + (time ? ' ' + time : '') + '</div>' +
    '<div class="msg-bubble">' + escapeHtml(content) + '</div>';

  $chatMessages.appendChild(div);
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
    el.classList.toggle('active', el.textContent.replace('×', '').trim() === (currentPrivate ? '' : currentRoom));
  });
  document.querySelectorAll('.user-item').forEach(el => {
    el.classList.toggle('active', el.textContent.trim() === currentPrivate);
  });
}

function escapeHtml(s) {
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}

// ── Heartbeat ──
let pingTimer = null;
function startPing() {
  if (pingTimer) clearInterval(pingTimer);
  pingTimer = setInterval(() => {
    send({ type: 'ping' });
    refreshOnlineUsers();
  }, 15000);
}

// ── Logout ──
$logoutBtn.onclick = () => {
  if (ws) ws.close();
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
