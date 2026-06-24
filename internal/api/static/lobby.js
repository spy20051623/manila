let room = null;
let roomSocket = null;
let reconnectTimer = null;
let roomToken = new URLSearchParams(window.location.search).get("token") || localStorage.getItem("manilaRoomToken") || "";
let toastTimer = null;

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
  const data = await api("/room/state");
  applyRoom(data.room || null);
}

function applyRoom(nextRoom) {
  room = nextRoom || null;
  if (!room?.participant?.joined && roomToken) {
    clearRoomToken();
  } else if (room?.participant?.joined) {
    syncTokenToURL();
  }
  renderLobby();
}

function connectRoomSocket() {
  clearTimeout(reconnectTimer);
  if (roomSocket) {
    roomSocket.onclose = null;
    roomSocket.close();
  }
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const url = `${protocol}//${window.location.host}/room/ws?token=${encodeURIComponent(roomToken || "")}`;
  roomSocket = new WebSocket(url);
  roomSocket.onmessage = (event) => {
    const data = JSON.parse(event.data || "{}");
    applyRoom(data.room || null);
  };
  roomSocket.onclose = () => {
    reconnectTimer = setTimeout(connectRoomSocket, 1200);
  };
}

function renderLobby() {
  const joined = Boolean(room?.participant?.joined);
  $("lobbyJoinBtn").hidden = joined;
  $("lobbyRenameBtn").hidden = !joined;
  $("lobbyStatusText").textContent = roomStatusText();
  $("lobbyJoinedText").textContent = participantText();
  $("lobbySeats").innerHTML = playerOrder.map(lobbySeatHTML).join("");
  $("lobbySeats").querySelectorAll("[data-room-action]").forEach((button) => {
    button.onclick = () => handleLobbyAction(button.dataset.roomAction, Number(button.dataset.playerId)).catch(showError);
  });
  renderGameOverlay();
  renderCompletedGames();
}

function renderGameOverlay() {
  const overlay = $("lobbyGameOverlay");
  const inProgress = room?.status === "inProgress" && room?.gameId;
  overlay.hidden = !inProgress;
  $("enterGameBtn").onclick = () => {
    if (!room?.gameId) return;
    window.location.href = gameURL(room.gameId);
  };
}

function renderCompletedGames() {
  const list = $("completedGamesList");
  const games = Array.isArray(room?.completedGames) ? room.completedGames : [];
  if (!games.length) {
    list.innerHTML = `<div class="completed-games-empty">暂无已完成对局</div>`;
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
    : "暂无结算分数";
  const roundText = game.roundNumber ? `第 ${Number(game.roundNumber)} 轮结束` : "终局";
  return `<button class="completed-game-card" data-game-id="${escapeHTML(game.gameId || "")}" type="button">
    <span class="completed-game-title">${escapeHTML(game.gameId || "旧对局")}</span>
    <span class="completed-game-round">${escapeHTML(roundText)}</span>
    <span class="completed-game-scores">${escapeHTML(scoreText)}</span>
  </button>`;
}

function gameURL(gameId) {
  const tokenParam = roomToken ? `&token=${encodeURIComponent(roomToken)}` : "";
  return `/game.html?game=${encodeURIComponent(gameId)}${tokenParam}`;
}

function roomStatusText() {
  if (!room) return "正在连接房间";
  if (room.status === "inProgress") return "对局进行中";
  if (room.status === "closing") return `${room.closeReason || "房间关闭中"} · ${room.closingSeconds || 0}s`;
  return "准备大厅";
}

function participantText() {
  if (!room?.participant?.joined) return "观战身份";
  const name = room.participant.name || "玩家";
  const playerId = Number(room.participant.playerId || 0);
  return playerId ? `${name} · P${playerId}` : `${name} · 尚未选择座位`;
}

function lobbySeatHTML(id) {
  const seat = (room?.seats || []).find((item) => Number(item.playerId) === id) || { playerId: id, type: "empty" };
  const isYou = Boolean(seat.isYou);
  const online = seat.type !== "human" || seat.online !== false;
  const controls = lobbySeatControls(id, seat, isYou);
  return `<article class="lobby-seat ${seat.type} ${isYou ? "you" : ""} ${online ? "" : "offline"}">
    <div class="lobby-seat-head"><span>P${id}</span><strong title="${escapeHTML(seatLabel(seat, isYou))}">${escapeHTML(seatLabel(seat, isYou))}</strong></div>
    <div class="lobby-seat-state">${escapeHTML(seatStateText(seat, online))}</div>
    <div class="lobby-seat-actions">${controls.join("")}</div>
  </article>`;
}

function lobbySeatControls(id, seat, isYou) {
  if (room?.status !== "waiting" || !room?.participant?.joined) return [];
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
  if (action === "claim") await api(`/room/seats/${playerId}/claim`, { method: "POST" });
  if (action === "leave") await api(`/room/seats/${playerId}/leave`, { method: "POST" });
  if (action === "ready") await api("/room/ready", { method: "POST" });
  if (action === "unready") await api("/room/unready", { method: "POST" });
  if (action === "addAI") await api(`/room/seats/${playerId}/ai`, { method: "POST" });
  if (action === "removeAI") await api(`/room/seats/${playerId}/ai`, { method: "DELETE" });
  await refresh();
}

async function joinRoom() {
  const latest = await api("/room/state");
  const suggested = latest.room?.suggestedName || "Player 1";
  const name = await openNameDialog({ mode: "join", initialName: suggested });
  if (!name) return;
  const data = await api("/room/join", { method: "POST", body: JSON.stringify({ name }) });
  roomToken = data.token || "";
  if (roomToken) localStorage.setItem("manilaRoomToken", roomToken);
  syncTokenToURL();
  connectRoomSocket();
  applyRoom(data.room || null);
}

async function renameRoomParticipant() {
  const current = room?.participant?.name || "";
  const name = await openNameDialog({ mode: "rename", initialName: current });
  if (!name) return;
  const data = await api("/room/name", { method: "PATCH", body: JSON.stringify({ name }) });
  applyRoom(data.room || null);
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
  if (!roomToken) return;
  const url = new URL(window.location.href);
  url.searchParams.set("token", roomToken);
  window.history.replaceState(null, "", url);
}

function clearRoomToken() {
  roomToken = "";
  localStorage.removeItem("manilaRoomToken");
  const url = new URL(window.location.href);
  url.searchParams.delete("token");
  window.history.replaceState(null, "", url);
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

$("lobbyJoinBtn").onclick = () => joinRoom().catch(showError);
$("lobbyRenameBtn").onclick = () => renameRoomParticipant().catch(showError);
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

connectRoomSocket();
refresh().catch(showError);
