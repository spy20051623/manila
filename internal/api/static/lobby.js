let lobby = null;
let room = null;
let lobbySocket = null;
let roomSocket = null;
let lobbyReconnectTimer = null;
let roomReconnectTimer = null;
let toastTimer = null;

const params = new URLSearchParams(window.location.search);
let selectedRoomId = params.get("room") || "";
let roomToken = params.get("token") || localStorage.getItem("manilaRoomToken") || "";

const MAX_PLAYER_NAME_LENGTH = 12;
const playerOrder = [1, 2, 3, 4];
const $ = (id) => document.getElementById(id);

if (roomToken) localStorage.setItem("manilaRoomToken", roomToken);

async function api(path, options = {}) {
  const headers = { "Content-Type": "application/json", ...(options.headers || {}) };
  if (roomToken) headers.Authorization = `Bearer ${roomToken}`;
  const res = await fetch(path, { ...options, headers });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.message || res.statusText);
  return data;
}

async function refresh() {
  const lobbyData = await api("/lobby/state");
  applyLobby(lobbyData.lobby || null);
  if (selectedRoomId) {
    try {
      const roomData = await api(`/rooms/${encodeURIComponent(selectedRoomId)}/state`);
      applyRoom(roomData.room || null);
    } catch (err) {
      leaveRoomView();
    }
  } else {
    room = null;
    renderLobby();
  }
}

function applyLobby(nextLobby) {
  lobby = nextLobby || null;
  if (roomToken && lobby && !lobby.participant?.joined) {
    clearRoomToken();
  } else if (lobby?.participant?.joined) {
    syncTokenToURL();
  }
  if (selectedRoomId && lobby && !lobby.rooms?.some((item) => item.roomId === selectedRoomId)) {
    leaveRoomView();
    showToast("房间已关闭");
  }
  renderLobby();
}

function applyRoom(nextRoom) {
  room = nextRoom || null;
  if (room?.roomId && room.roomId !== selectedRoomId) {
    selectedRoomId = room.roomId;
    syncTokenToURL();
  }
  if (roomToken && room && !room.participant?.joined && !isAdmin()) {
    clearRoomToken();
  }
  renderLobby();
}

function connectLobbySocket() {
  clearTimeout(lobbyReconnectTimer);
  if (lobbySocket) {
    lobbySocket.onclose = null;
    lobbySocket.close();
  }
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const url = `${protocol}//${window.location.host}/lobby/ws?token=${encodeURIComponent(roomToken || "")}`;
  lobbySocket = new WebSocket(url);
  lobbySocket.onmessage = (event) => {
    const data = JSON.parse(event.data || "{}");
    applyLobby(data.lobby || null);
  };
  lobbySocket.onclose = () => {
    lobbyReconnectTimer = setTimeout(connectLobbySocket, 1200);
  };
}

function connectRoomSocket() {
  clearTimeout(roomReconnectTimer);
  if (roomSocket) {
    roomSocket.onclose = null;
    roomSocket.close();
    roomSocket = null;
  }
  if (!selectedRoomId) return;
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const url = `${protocol}//${window.location.host}/rooms/${encodeURIComponent(selectedRoomId)}/ws?token=${encodeURIComponent(roomToken || "")}`;
  roomSocket = new WebSocket(url);
  roomSocket.onmessage = (event) => {
    const data = JSON.parse(event.data || "{}");
    if (data.room) applyRoom(data.room);
  };
  roomSocket.onclose = () => {
    roomReconnectTimer = setTimeout(connectRoomSocket, 1200);
  };
}

function renderLobby() {
  const joined = Boolean(lobby?.participant?.joined);
  const admin = isAdmin();
  $("lobbyJoinBtn").hidden = joined;
  $("lobbyRenameBtn").hidden = !joined || admin;
  $("lobbyCreateRoomBtn").hidden = admin || !joined || Boolean(selectedRoomId);
  $("lobbyBackBtn").hidden = !selectedRoomId;
  $("lobbyStatusText").textContent = selectedRoomId ? roomStatusText() : "大厅";
  $("lobbyJoinedText").textContent = participantText();
  $("roomListSection").hidden = Boolean(selectedRoomId);
  document.querySelector(".lobby-seat-zone").hidden = !selectedRoomId;

  if (selectedRoomId) {
    $("lobbySeats").innerHTML = playerOrder.map(lobbySeatHTML).join("");
    $("lobbySeats").querySelectorAll("[data-room-action]").forEach((button) => {
      button.onclick = () => handleLobbyAction(button.dataset.roomAction, Number(button.dataset.playerId)).catch(showError);
    });
  } else {
    $("lobbySeats").innerHTML = "";
  }
  renderRoomList();
  renderGameOverlay();
  renderCompletedGames();
}

function renderRoomList() {
  const list = $("roomList");
  const rooms = Array.isArray(lobby?.rooms) ? lobby.rooms : [];
  if (selectedRoomId) {
    list.innerHTML = "";
    return;
  }
  if (!rooms.length) {
    list.innerHTML = `<div class="completed-games-empty">还没有房间</div>`;
    return;
  }
  list.innerHTML = rooms.map(roomCardHTML).join("");
  list.querySelectorAll("[data-room-enter]").forEach((button) => {
    button.onclick = () => enterRoom(button.dataset.roomEnter).catch(showError);
  });
  list.querySelectorAll("[data-room-close]").forEach((button) => {
    button.onclick = () => adminCloseRoom(button.dataset.roomClose).catch(showError);
  });
}

function roomCardHTML(summary) {
  const status = roomSummaryStatus(summary);
  const adminClose = summary.canAdminClose
    ? `<button data-room-close="${escapeHTML(summary.roomId || "")}" type="button">强制关闭</button>`
    : "";
  return `<article class="room-card">
    <div>
      <div class="room-card-title">${escapeHTML(summary.name || summary.roomId || "房间")}</div>
      <div class="room-card-meta">${Number(summary.humanPlayerCount || 0)} 人 / ${Number(summary.aiPlayerCount || 0)} AI · ${Number(summary.seatCount || 0)}/4 座</div>
    </div>
    <div class="room-card-actions">
      <span class="room-status-pill ${escapeHTML(summary.status || "")}">${escapeHTML(status)}</span>
      <button data-room-enter="${escapeHTML(summary.roomId || "")}" type="button">${summary.isMember ? "进入" : "加入"}</button>
      ${adminClose}
    </div>
  </article>`;
}

function roomSummaryStatus(summary) {
  if (summary.status === "inProgress") return "对局中";
  if (summary.status === "closing") return "关闭中";
  return "等待中";
}

function renderGameOverlay() {
  const overlay = $("lobbyGameOverlay");
  const inProgress = selectedRoomId && room?.status === "inProgress" && room?.gameId;
  overlay.hidden = !inProgress;
  $("enterGameBtn").onclick = () => {
    if (!room?.gameId) return;
    window.location.href = gameURL(room.gameId);
  };
}

function renderCompletedGames() {
  const list = $("completedGamesList");
  const games = Array.isArray(lobby?.completedGames) ? lobby.completedGames : [];
  if (!games.length) {
    list.innerHTML = `<div class="completed-games-empty">还没有结束的对局</div>`;
    return;
  }
  list.innerHTML = games.map(completedGameHTML).join("");
  list.querySelectorAll("[data-game-id]").forEach((button) => {
    button.onclick = () => {
      window.location.href = gameURL(button.dataset.gameId);
    };
  });
}

function completedGameHTML(game) {
  const scores = Array.isArray(game.scores) ? [...game.scores] : [];
  scores.sort((a, b) => Number(a.rank || 99) - Number(b.rank || 99));
  const scoreText = scores.length
    ? scores.map((score) => `第${Number(score.rank || 0)}名 P${Number(score.playerId || 0)} ${Number(score.wealth || 0)}分`).join(" · ")
    : "结算待生成";
  const roundText = game.roundNumber ? `第 ${Number(game.roundNumber)} 轮结束` : "终局";
  return `<button class="completed-game-card" data-game-id="${escapeHTML(game.gameId || "")}" type="button">
    <span class="completed-game-title">${escapeHTML(game.gameId || "历史对局")}</span>
    <span class="completed-game-round">${escapeHTML(roundText)}</span>
    <span class="completed-game-scores">${escapeHTML(scoreText)}</span>
  </button>`;
}

function gameURL(gameId) {
  const url = new URL("/game.html", window.location.origin);
  url.searchParams.set("game", gameId);
  if (roomToken) url.searchParams.set("token", roomToken);
  if (selectedRoomId) url.searchParams.set("room", selectedRoomId);
  return `${url.pathname}${url.search}`;
}

function roomStatusText() {
  if (!room) return "正在连接房间";
  return room.name || "房间";
}

function participantText() {
  if (isAdmin()) return "管理员";
  const participant = lobby?.participant;
  if (!participant?.joined) return "观战身份";
  return participant.name || "玩家";
}

function lobbySeatHTML(id) {
  const seat = (room?.seats || []).find((item) => Number(item.playerId) === id) || { playerId: id, type: "empty" };
  const isYou = Boolean(seat.isYou);
  const online = seat.type !== "human" || seat.online !== false;
  const controls = lobbySeatControls(id, seat, isYou);
  return `<article class="lobby-seat ${escapeHTML(seat.type)} ${isYou ? "you" : ""} ${online ? "" : "offline"}">
    <div class="lobby-seat-head"><span>P${id}</span><strong title="${escapeHTML(seatLabel(seat, isYou))}">${escapeHTML(seatLabel(seat, isYou))}</strong></div>
    <div class="lobby-seat-state">${escapeHTML(seatStateText(seat, online))}</div>
    <div class="lobby-seat-actions">${controls.join("")}</div>
  </article>`;
}

function lobbySeatControls(id, seat, isYou) {
  if (isAdmin() || room?.status !== "waiting" || !room?.participant?.joined) return [];
  const seated = Number(room?.participant?.playerId || 0);
  const canCancelReady = Boolean(room?.canCancelReady);
  const controls = [];
  if (isYou && canCancelReady) {
    controls.push(lobbyButton("unready", id, "取消准备"));
    return controls;
  }
  if (seat.type === "empty") {
    if (!seated) controls.push(lobbyButton("claim", id, "选择座位"));
    if (seated && !canCancelReady) controls.push(lobbyButton("claim", id, "切换座位"));
    if (room?.canManageAI) controls.push(lobbyButton("addAI", id, "放置 AI"));
  }
  if (seat.type === "ai" && room?.canManageAI) {
    controls.push(lobbyButton("removeAI", id, "撤销 AI"));
  }
  if (isYou && room?.canReady) controls.push(lobbyButton("ready", id, "准备"));
  if (isYou && room?.canLeaveSeat) controls.push(lobbyButton("leave", id, "放弃座位"));
  return controls;
}

function lobbyButton(action, playerId, label) {
  return `<button data-room-action="${action}" data-player-id="${playerId}" type="button">${label}</button>`;
}

function seatLabel(seat, isYou) {
  if (seat.type === "human") return seat.name || (isYou ? "你" : "真人玩家");
  if (seat.type === "ai") return "AI";
  return "空位";
}

function seatStateText(seat, online) {
  if (seat.type === "human") {
    if (!online) return "离线";
    return seat.ready ? "已准备" : "未准备";
  }
  if (seat.type === "ai") return "自动准备";
  return "等待选择";
}

async function handleLobbyAction(action, playerId) {
  await ensurePlayerToken();
  if (!selectedRoomId) throw new Error("请先进入房间");
  const base = `/rooms/${encodeURIComponent(selectedRoomId)}`;
  if (action === "claim") await api(`${base}/seats/${playerId}/claim`, { method: "POST" });
  if (action === "leave") await api(`${base}/seats/${playerId}/leave`, { method: "POST" });
  if (action === "ready") await api(`${base}/ready`, { method: "POST" });
  if (action === "unready") await api(`${base}/unready`, { method: "POST" });
  if (action === "addAI") await api(`${base}/seats/${playerId}/ai`, { method: "POST" });
  if (action === "removeAI") await api(`${base}/seats/${playerId}/ai`, { method: "DELETE" });
  await refresh();
}

async function joinLobby() {
  await ensurePlayerToken({ forceNameDialog: true });
}

async function ensurePlayerToken(options = {}) {
  if (isAdmin()) throw new Error("管理员身份不能进行玩家操作");
  if (roomToken && lobby?.participant?.joined && !options.forceNameDialog) return true;
  const latest = await api("/lobby/state");
  applyLobby(latest.lobby || null);
  if (roomToken && lobby?.participant?.joined && !options.forceNameDialog) return true;
  const suggested = latest.lobby?.suggestedName || "Player 1";
  const name = await openNameDialog({ mode: "join", initialName: suggested });
  if (!name) return false;
  const data = await api("/lobby/join", { method: "POST", body: JSON.stringify({ name }) });
  roomToken = data.token || "";
  if (roomToken) localStorage.setItem("manilaRoomToken", roomToken);
  syncTokenToURL();
  connectLobbySocket();
  applyLobby(data.lobby || null);
  return true;
}

async function createRoom() {
  if (!(await ensurePlayerToken())) return;
  const data = await api("/rooms", { method: "POST" });
  selectedRoomId = data.roomId || data.room?.roomId || "";
  room = data.room || null;
  syncTokenToURL();
  connectRoomSocket();
  await refresh();
}

async function enterRoom(roomID) {
  if (!(await ensurePlayerToken())) return;
  await api(`/rooms/${encodeURIComponent(roomID)}/join`, { method: "POST" });
  selectedRoomId = roomID;
  syncTokenToURL();
  connectRoomSocket();
  await refresh();
}

async function adminCloseRoom(roomID) {
  if (!isAdmin()) throw new Error("需要管理员身份");
  await api(`/rooms/${encodeURIComponent(roomID)}/admin/close`, { method: "POST" });
  if (selectedRoomId === roomID) {
    selectedRoomId = "";
    room = null;
    syncTokenToURL();
    connectRoomSocket();
  }
  await refresh();
}

function backToLobby() {
  leaveRoomView();
}

function leaveRoomView() {
  selectedRoomId = "";
  room = null;
  syncTokenToURL();
  connectRoomSocket();
  renderLobby();
}

async function renameRoomParticipant() {
  if (isAdmin()) return;
  const current = lobby?.participant?.name || "";
  const name = await openNameDialog({ mode: "rename", initialName: current });
  if (!name) return;
  const data = await api("/lobby/name", { method: "PATCH", body: JSON.stringify({ name }) });
  applyLobby(data.lobby || null);
  if (selectedRoomId) await refresh();
}

function openNameDialog({ mode, initialName }) {
  $("nameDialogTitle").textContent = mode === "rename" ? "更改名字" : "玩家名字";
  $("nameDialogInput").value = (initialName || "").slice(0, MAX_PLAYER_NAME_LENGTH);
  updateNameCounter();
  $("nameDialogOverlay").hidden = false;
  $("nameDialogInput").focus();
  $("nameDialogInput").select();
  return new Promise((resolve) => {
    window.pendingNameDialogResolve = resolve;
  });
}

function closeNameDialog(value = "") {
  $("nameDialogOverlay").hidden = true;
  const resolve = window.pendingNameDialogResolve;
  window.pendingNameDialogResolve = null;
  resolve?.(value);
}

function updateNameCounter() {
  const value = $("nameDialogInput").value.slice(0, MAX_PLAYER_NAME_LENGTH);
  $("nameDialogInput").value = value;
  $("nameDialogCounter").textContent = `${value.length}/${MAX_PLAYER_NAME_LENGTH}`;
}

function syncTokenToURL() {
  const url = new URL(window.location.href);
  if (roomToken) url.searchParams.set("token", roomToken);
  else url.searchParams.delete("token");
  if (selectedRoomId) url.searchParams.set("room", selectedRoomId);
  else url.searchParams.delete("room");
  window.history.replaceState(null, "", url);
}

function clearRoomToken() {
  roomToken = "";
  localStorage.removeItem("manilaRoomToken");
  syncTokenToURL();
}

function isAdmin() {
  return Boolean(lobby?.participant?.isAdmin);
}

function showError(err) {
  showToast(err.message || String(err));
}

function showToast(message) {
  const el = $("toast");
  el.textContent = message;
  el.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.remove("show"), 3200);
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

$("lobbyJoinBtn").onclick = () => joinLobby().catch(showError);
$("lobbyRenameBtn").onclick = () => renameRoomParticipant().catch(showError);
$("lobbyCreateRoomBtn").onclick = () => createRoom().catch(showError);
$("lobbyBackBtn").onclick = backToLobby;
$("nameDialogInput").addEventListener("input", updateNameCounter);
$("nameDialogCloseBtn").onclick = () => closeNameDialog("");
$("nameDialogCancelBtn").onclick = () => closeNameDialog("");
$("nameDialogOverlay").addEventListener("click", (event) => {
  if (event.target === $("nameDialogOverlay")) closeNameDialog("");
});
$("nameDialogForm").addEventListener("submit", (event) => {
  event.preventDefault();
  const value = $("nameDialogInput").value.trim().slice(0, MAX_PLAYER_NAME_LENGTH);
  if (!value) {
    showToast("请输入名字");
    return;
  }
  closeNameDialog(value);
});

connectLobbySocket();
connectRoomSocket();
refresh().catch(showError);
