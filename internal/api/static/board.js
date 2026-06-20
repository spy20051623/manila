let gameId = localStorage.getItem("manilaBoardGameId") || "";
let state = null;
let actions = [];
let aiBusy = false;
let selectedGoods = new Set();
let shipStarts = {};
let navigatorMoves = {};
let toastTimer = null;
let dismissedSettlementKey = "";
const DEBUG_SHOW_ALL_HOTSPOTS = false;

const $ = (id) => document.getElementById(id);
const marketValues = [0, 5, 10, 20, 30];
const goodsOrder = [1, 2, 3, 4];
const playerOrder = [1, 2, 3, 4];
const playerColors = {
  1: "#c96a3d",
  2: "#c5862e",
  3: "#8988cf",
  4: "#7bb964",
};
const goodsMeta = {
  1: { name: "人参", className: "ginseng", payout: 18, costs: [1, 2, 3] },
  2: { name: "肉豆蔻", className: "nutmeg", payout: 24, costs: [2, 3, 4] },
  3: { name: "丝绸", className: "silk", payout: 30, costs: [3, 4, 5] },
  4: { name: "玉石", className: "jade", payout: 36, costs: [3, 4, 5, 5] },
};

async function api(path, options = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.message || res.statusText);
  return data;
}

function localPlayerStorageKey(id = gameId) {
  return `manilaBoardLocalPlayer:${id || "default"}`;
}

function setLocalPlayerId(playerId, id = gameId) {
  localStorage.setItem(localPlayerStorageKey(id), String(playerId));
}

function getLocalPlayerId() {
  const stored = Number(localStorage.getItem(localPlayerStorageKey()) || 1);
  return playerOrder.includes(stored) ? stored : 1;
}

function viewerQuery() {
  return `viewerId=${getLocalPlayerId()}`;
}

async function createGame() {
  const created = await api("/games", { method: "POST", body: JSON.stringify({}) });
  gameId = created.game.gameId;
  localStorage.setItem("manilaBoardGameId", gameId);
  setLocalPlayerId(1, gameId);
  await api(`/games/${gameId}/start?${viewerQuery()}`, { method: "POST" });
  resetTransientControls();
  await refresh();
  await maybeAutoAI();
}

async function refresh(options = {}) {
  if (!gameId) {
    renderEmpty();
    return;
  }
  const data = await api(`/games/${gameId}/state?${viewerQuery()}`);
  state = data.game;
  await loadLegalActions();
  syncTransientControls();
  render();
  if (options.auto !== false) {
    setTimeout(() => maybeAutoAI().catch(showError), 120);
  }
}

async function loadLegalActions() {
  actions = [];
  if (!state || !state.currentPlayer || state.status === "ended") return;
  const localPlayerId = getLocalPlayerId();
  if (Number(state.currentPlayer) !== localPlayerId) return;
  const data = await api(`/games/${gameId}/players/${localPlayerId}/actions?${viewerQuery()}`);
  actions = data.actions || [];
}

async function submitAction(action, payloadOverride) {
  if (!state || !action) return;
  const localPlayerId = getLocalPlayerId();
  if (Number(state.currentPlayer) !== localPlayerId) {
    showToast(`等待 P${state.currentPlayer} 行动。`);
    return;
  }
  const payload = payloadOverride === undefined ? normalizePayload(action.payload || {}) : payloadOverride;
  const requestBody = {
    playerId: localPlayerId,
    type: action.type,
    payload,
    expectedEventSeq: state.eventSeq,
  };
  await api(`/games/${gameId}/actions?${viewerQuery()}`, { method: "POST", body: JSON.stringify(requestBody) });
  resetTransientControls();
  await refresh();
}

async function aiStep() {
  if (!state || !state.currentPlayer || aiBusy) return;
  aiBusy = true;
  setBusyButtons(true);
  try {
    await api(`/games/${gameId}/players/${state.currentPlayer}/trained-ai?${viewerQuery()}`, { method: "POST" });
    const data = await api(`/games/${gameId}/state?${viewerQuery()}`);
    state = data.game;
    await loadLegalActions();
    syncTransientControls();
    render();
    await runAIOpponentsUntilHuman(80);
  } finally {
    aiBusy = false;
    setBusyButtons(false);
  }
}

async function aiStepInternal() {
  await api(`/games/${gameId}/players/${state.currentPlayer}/trained-ai?${viewerQuery()}`, { method: "POST" });
  const data = await api(`/games/${gameId}/state?${viewerQuery()}`);
  state = data.game;
  await loadLegalActions();
  syncTransientControls();
  render();
}

async function maybeAutoAI() {
  if (aiBusy || !state || state.status === "ended") return;
  if (![2, 3, 4].includes(Number(state.currentPlayer))) return;
  aiBusy = true;
  setBusyButtons(true);
  try {
    await runAIOpponentsUntilHuman(80);
  } finally {
    aiBusy = false;
    setBusyButtons(false);
  }
}

async function runAIOpponentsUntilHuman(maxSteps) {
  for (let i = 0; i < maxSteps; i++) {
    if (!state || state.status === "ended" || ![2, 3, 4].includes(Number(state.currentPlayer))) break;
    await aiStepInternal();
  }
}

async function aiAdvanceRound() {
  if (!state || aiBusy) return;
  aiBusy = true;
  setBusyButtons(true);
  let leftStartingReview = state.phase !== "RoundReview";
  try {
    for (let i = 0; i < 600; i++) {
      if (!state || state.status === "ended" || !state.currentPlayer) break;
      if (state.phase === "RoundReview" && leftStartingReview) break;
      await aiStepInternal();
      if (state.phase !== "RoundReview") leftStartingReview = true;
    }
  } finally {
    aiBusy = false;
    setBusyButtons(false);
  }
}

function render() {
  if (!state) {
    renderEmpty();
    return;
  }
  $("gameIdText").textContent = state.gameId || "-";
  $("roundText").textContent = state.roundNumber || "-";
  $("phaseText").textContent = phaseName(state);
  $("currentPlayerText").textContent = state.currentPlayer ? `P${state.currentPlayer}` : "-";
  $("harborMasterText").textContent = state.harborMaster ? `P${state.harborMaster}` : "-";

  renderRouteNumbers();
  renderGoodsMarket();
  renderPorts();
  renderDocks();
  renderSea();
  renderTopShips();
  renderSpecialSlots();
  renderPlayersCompact();
  renderContextControls();
  renderActionList();
  renderEvents();
  renderSettlementOverlay();
}

function renderEmpty() {
  for (const id of ["gameIdText", "roundText", "phaseText", "currentPlayerText", "harborMasterText"]) {
    $(id).textContent = "-";
  }
  $("goodsMarket").innerHTML = "";
  $("portSlots").innerHTML = "";
  $("dockSlots").innerHTML = "";
  $("seaLanes").innerHTML = `<div class="empty-note">欢迎来到马尼拉！</div>`;
  $("pirateSlots").innerHTML = "";
  $("navigatorSlots").innerHTML = "";
  $("insuranceSlot").innerHTML = "";
  $("topShips").innerHTML = "";
  $("playersList").innerHTML = "";
  $("contextControls").innerHTML = "";
  $("actionsList").innerHTML = `<div class="empty-note">尚无游戏。</div>`;
  $("eventsList").innerHTML = "";
  renderSettlementOverlay();
  renderRouteNumbers();
}

function renderSettlementOverlay() {
  const overlay = $("settlementOverlay");
  const scores = finalScoresForDisplay();
  const key = scores.length ? `${state?.gameId || ""}:${state?.eventSeq || ""}:settlement` : "";
  if (!scores.length || key === dismissedSettlementKey) {
    overlay.hidden = true;
    overlay.innerHTML = "";
    return;
  }
  overlay.hidden = false;
  const winner = scores[0];
  const others = scores.slice(1, 4);
  overlay.innerHTML = `
    <div class="settlement-card" role="dialog" aria-label="游戏结算">
      <button class="settlement-close" type="button" aria-label="关闭结算画面"></button>
      <div class="winner-medal" aria-hidden="true">♛</div>
      <div class="settlement-eyebrow">游戏结束</div>
      <section class="winner-block">
        <div class="winner-rank">第1名</div>
        <div class="winner-player">P${winner.playerId}</div>
        <div class="winner-score">${winner.wealth} 分</div>
      </section>
      <section class="runner-list">
        ${others.map((score) => `
          <article class="runner-card">
            <div class="runner-rank">第${score.rank}名</div>
            <div class="runner-player">P${score.playerId}</div>
            <div class="runner-score">${score.wealth} 分</div>
          </article>
        `).join("")}
      </section>
    </div>`;
  overlay.querySelector(".settlement-close")?.addEventListener("click", () => {
    dismissedSettlementKey = key;
    overlay.hidden = true;
  });
}

function finalScoresForDisplay() {
  if (Array.isArray(state?.finalScores) && state.finalScores.length) {
    return [...state.finalScores].sort(compareScores);
  }
  const ended = [...(state?.events || [])].reverse().find((event) => event.type === "GameEnded");
  const scores = ended?.data?.scores;
  return Array.isArray(scores) ? [...scores].sort(compareScores) : [];
}

function compareScores(a, b) {
  if (Number(a.rank) !== Number(b.rank)) return Number(a.rank) - Number(b.rank);
  if (Number(a.wealth) !== Number(b.wealth)) return Number(b.wealth) - Number(a.wealth);
  return Number(a.playerId) - Number(b.playerId);
}

function renderRouteNumbers() {
  $("routeNumbers").innerHTML = Array.from({ length: 14 }, (_, index) => `<span>${index}</span>`).join("");
}

function renderGoodsMarket() {
  const buyActions = actions.filter((a) => a.type === "BuyShare");
  $("goodsMarket").innerHTML = goodsOrder.map((id) => {
    const goods = getGoods(id);
    const meta = goodsMeta[id];
    const priceIndex = Number(goods?.marketValueIndex || 0);
    const buyAction = buyActions.find((a) => Number(a.payload?.goodsId) === id);
    const selectable = state.phase === "HarborMasterSelectGoods";
    const classes = [
      "goods-card",
      meta.className,
      DEBUG_SHOW_ALL_HOTSPOTS ? "debug-hotspot" : "",
      buyAction || selectable ? "actionable" : "",
      selectedGoods.has(id) ? "selected" : "",
    ].filter(Boolean).join(" ");
    const track = marketValues.map((value, index) => {
      return `<span class="market-step ${index === priceIndex ? "active" : ""}">${value}</span>`;
    }).join("");
    return `<article class="${classes}" data-goods-id="${id}">
      <h3>${meta.name}</h3>
      <div class="market-track">${track}</div>
      <div class="goods-meta">
        <span>股份 ${goods?.sharesRemaining ?? "-"}</span>
        <span>船奖 ${goods?.shipPayout ?? meta.payout}</span>
      </div>
      <div class="board-share-badge">剩 ${goods?.sharesRemaining ?? "-"}</div>
    </article>`;
  }).join("");

  $("goodsMarket").querySelectorAll(".goods-card").forEach((card) => {
    card.addEventListener("click", () => handleGoodsClick(Number(card.dataset.goodsId)).catch(showError));
  });
}

async function handleGoodsClick(goodsId) {
  const buyAction = actions.find((a) => a.type === "BuyShare" && Number(a.payload?.goodsId) === goodsId);
  if (buyAction) {
    await submitAction(buyAction);
    return;
  }
  if (state.phase !== "HarborMasterSelectGoods") return;
  if (selectedGoods.has(goodsId)) {
    selectedGoods.delete(goodsId);
  } else if (selectedGoods.size < 3) {
    selectedGoods.add(goodsId);
  } else {
    showToast("本轮只能选择 3 种货物出航。");
  }
  renderGoodsMarket();
  renderContextControls();
}

function renderPorts() {
  $("portSlots").innerHTML = ["A", "B", "C"].map((slotId) => {
    const slot = state.board?.ports?.[slotId];
    const ship = shipBySlot("port", slotId);
    const action = placementAction("port", slotId) || destinationAction("port");
    return boardSlotHTML({
      id: slotId,
      title: `港口 ${slotId}`,
      meta: `花费 ${slot?.cost ?? "-"} / 成功 ${slot?.reward ?? "-"}`,
      labels: slotLabels(slot?.cost, slot?.reward),
      piece: slot?.occupant,
      action,
      ship,
      kind: "port",
    });
  }).join("");
  wireBoardSlots("portSlots");
}

function renderDocks() {
  $("dockSlots").innerHTML = ["A", "B", "C"].map((slotId) => {
    const slot = state.board?.docks?.[slotId];
    const ship = shipBySlot("dock", slotId);
    const action = placementAction("dock", slotId) || destinationAction("dock");
    return boardSlotHTML({
      id: slotId,
      title: `船坞 ${slotId}`,
      meta: `花费 ${slot?.cost ?? "-"} / 成功 ${slot?.reward ?? "-"}`,
      labels: slotLabels(slot?.cost, slot?.reward),
      piece: slot?.occupant,
      action,
      ship,
      kind: "dock",
    });
  }).join("");
  wireBoardSlots("dockSlots");
}

function boardSlotHTML({ id, title, meta, labels, piece, action, ship, kind }) {
  const actionKey = action ? actionKeyFor(action) : "";
  const classes = ["board-slot", piece ? "filled" : "", ship ? "settled" : "", action ? "actionable" : ""].filter(Boolean).join(" ");
  const debugClass = DEBUG_SHOW_ALL_HOTSPOTS ? " debug-hotspot" : "";
  return `<div class="${classes}${debugClass}" data-action-key="${escapeAttr(actionKey)}" data-kind="${kind}" data-slot-id="${id}">
    <div class="slot-name"><span>${title}</span><span>${piece ? pieceText(piece) : "空"}</span></div>
    <div class="slot-meta">${meta}</div>
    <div class="piece-row">${piece ? pieceHTML(piece) : pieceHTML(null)}</div>
    ${!piece ? slotLabelsHTML(labels) : ""}
    ${ship ? `<div class="settled-ship">${goodsName(ship.shipId)} ${ship.status === "arrived" ? "到港" : "进坞"}</div>` : ""}
  </div>`;
}

function wireBoardSlots(containerId) {
  $(containerId).querySelectorAll("[data-action-key]").forEach((el) => {
    const action = actionByKey(el.dataset.actionKey);
    if (!action) return;
    el.addEventListener("click", () => submitAction(action).catch(showError));
  });
}

function renderSea() {
  const selected = selectedShipIds();
  if (selected.length === 0) {
    $("seaLanes").innerHTML = `<div class="empty-note">欢迎来到马尼拉！</div>`;
    return;
  }

  $("seaLanes").innerHTML = selected.map((shipId, index) => {
    const ship = getShip(shipId);
    return ship ? renderShip(ship, index) : "";
  }).join("");

  $("seaLanes").querySelectorAll(".ship[data-action-key]").forEach((el) => {
    const action = actionByKey(el.dataset.actionKey);
    if (!action) return;
    el.addEventListener("click", () => submitAction(action).catch(showError));
  });
}

function renderTopShips() {
  const selected = (state.round?.selectedGoods || []).map(Number).slice(0, 3);
  if (selected.length === 0) {
    $("topShips").innerHTML = `<div class="empty-note">本轮尚未选择货船。</div>`;
    return;
  }
  $("topShips").innerHTML = selected.map((shipId) => {
    const ship = getShip(shipId) || { shipId, occupants: [] };
    return renderTopShip(ship);
  }).join("");
}

function renderTopShip(ship) {
  const shipId = Number(ship.shipId);
  const meta = goodsMeta[shipId];
  if (!meta) return "";
  const occupants = ship.occupants || [];
  const slots = boardingCosts(shipId).map((cost, index) => {
    const occupant = occupants[index];
    const style = occupant ? pieceStyle(occupant) : "";
    return `<span class="top-ship-seat ${occupant ? "occupied" : ""}" style="${style}">${occupant ? `P${occupant.playerId}` : cost}</span>`;
  }).join("");
  const arrived = ship.status === "arrived";
  return `<article class="top-ship ${meta.className} ${arrived ? "arrived" : ""}">
    <div class="top-ship-head">
      <span>${meta.name}</span>
      <strong class="ship-reward-label">收益 ${meta.payout}</strong>
    </div>
    <div class="top-ship-body">${slots}</div>
  </article>`;
}

function renderShip(ship, fallbackIndex) {
  const shipId = Number(ship.shipId);
  const meta = goodsMeta[shipId];
  const placement = placementAction("ship", shipId);
  const pirate = actions.find((a) => a.type === "PirateBoard" && Number(a.payload?.shipId) === shipId);
  const action = placement || pirate;
  const routeIndex = Number(ship.routeIndex || fallbackIndex + 1);
  const previewStart = state.phase === "HarborMasterSetShips" ? Number(shipStarts[shipId]) : NaN;
  const basePosition = Number.isFinite(previewStart) ? clampIntegerValue(previewStart, 0, 5) : Number(ship.position || 0);
  const displayPosition = shipTextPosition(basePosition);
  const point = shipPoint(shipBoardPosition(basePosition, ship.status), routeIndex);
  const actionKey = action ? actionKeyFor(action) : "";
  const classes = ["ship", meta.className, action ? "actionable" : ""].filter(Boolean).join(" ");
  const icon = goodsIconSrc(shipId);
  return `<div class="${classes}" data-action-key="${escapeAttr(actionKey)}" style="left: ${point.x}%; top: ${point.y}%;">
    <div class="ship-text">
      <div class="ship-title">${meta.name}</div>
      <div class="ship-line">位置 ${displayPosition}</div>
      <div class="ship-line">${shipStatusName(ship.status)}</div>
    </div>
    ${icon ? `<img class="ship-goods-icon" src="${icon}" alt="" aria-hidden="true" loading="lazy">` : ""}
  </div>`;
}

function shipPoint(position, routeIndex) {
  const x0 = 25.1;
  const x13 = 76.8;
  const step = (x13 - x0) / 13;
  const lanes = { 1: 31.8, 2: 46.6, 3: 61.6 };
  const x = x0 + step * Math.max(0, Math.min(14, position));
  const y = lanes[routeIndex] || lanes[1];
  return { x, y };
}

function shipBoardPosition(position, status) {
  const numericPosition = Number(position || 0);
  if (numericPosition > 13) return 14;
  if (status === "arrived" && numericPosition <= 0) return 14;
  return Math.max(0, Math.min(13, numericPosition));
}

function shipTextPosition(position) {
  return Math.max(0, Number(position || 0));
}

function renderSpecialSlots() {
  const piratePieces = pirateSlots();
  $("pirateSlots").innerHTML = [
    specialSlotHTML("船长", "花费 5", piratePieces[0], placementAction("pirate", 1), slotLabels(5, null)),
    specialSlotHTML("副手", "花费 5", piratePieces[1], placementAction("pirate", 2), slotLabels(5, null)),
  ].join("");

  $("navigatorSlots").innerHTML = [
    specialSlotHTML("小领航", "花费 2", state.board?.smallNavigator, placementAction("navigatorSmall", "small"), slotLabels(2, null)),
    specialSlotHTML("大领航", "花费 5", state.board?.bigNavigator, placementAction("navigatorBig", "big"), slotLabels(5, null)),
  ].join("");

  $("insuranceSlot").innerHTML = specialSlotHTML("保险商", "立即获得 10", state.board?.insurance, placementAction("insurance", "insurance"), [
    { type: "cost", text: "赔船坞" },
    { type: "reward", text: "收益 10" },
  ]);

  for (const id of ["pirateSlots", "navigatorSlots", "insuranceSlot"]) {
    wireBoardSlots(id);
  }
}

function specialSlotHTML(title, meta, piece, action, labels) {
  const actionKey = action ? actionKeyFor(action) : "";
  const classes = ["board-slot", piece ? "filled" : "", action ? "actionable" : ""].filter(Boolean).join(" ");
  const debugClass = DEBUG_SHOW_ALL_HOTSPOTS ? " debug-hotspot" : "";
  return `<div class="${classes}${debugClass}" data-action-key="${escapeAttr(actionKey)}">
    <div class="slot-name"><span>${title}</span><span>${piece ? pieceText(piece) : "空"}</span></div>
    <div class="slot-meta">${meta}</div>
    <div class="piece-row">${piece ? pieceHTML(piece) : pieceHTML(null)}</div>
    ${!piece ? slotLabelsHTML(labels) : ""}
  </div>`;
}

function slotLabels(cost, reward) {
  const labels = [];
  if (reward !== null && reward !== undefined && reward !== "-") labels.push({ type: "reward", text: `收益 ${reward}` });
  if (cost !== null && cost !== undefined && cost !== "-") labels.push({ type: "cost", text: `成本 ${cost}` });
  return labels;
}

function slotLabelsHTML(labels = []) {
  if (!labels.length) return "";
  return `<div class="slot-labels">${labels.map((label) => `<span class="slot-label ${label.type}">${label.text}</span>`).join("")}</div>`;
}

function renderPlayers() {
  const players = playerOrder.map((id) => state.players?.[id]).filter(Boolean);
  $("playersList").innerHTML = players.map((player) => {
    const current = Number(state.currentPlayer) === Number(player.playerId);
    return `<article class="player-card ${current ? "current" : ""}">
      <div class="player-head">
        <span>${pieceHTML({ playerId: player.playerId })} 玩家 ${player.playerId}</span>
        <span>${player.cash} 比索</span>
      </div>
      <div class="player-stats">可用同伙 ${player.availableAccomplices} · 已放置 ${player.placedAccomplices} · 抵押 ${player.mortgagedShareCount || 0}${player.bankruptThisRound ? " · 本轮破产" : ""}</div>
      <div class="share-line">${shareSummary(player)}</div>
    </article>`;
  }).join("");
}

function renderPlayersCompact() {
  const players = playerOrder.map((id) => state.players?.[id]).filter(Boolean);
  $("playersList").innerHTML = players.map((player) => {
    const current = Number(state.currentPlayer) === Number(player.playerId);
    return `<article class="player-card ${current ? "current" : ""}">
      <div class="player-head">
        <span>${pieceHTML({ playerId: player.playerId })} P${player.playerId}</span>
        <span>${player.cash} 比索</span>
      </div>
      <div class="share-line">${shareSummary(player)}</div>
    </article>`;
  }).join("");
}

function renderContextControls() {
  const box = $("contextControls");
  box.innerHTML = "";
  if (!state || actions.length === 0) return;

  const bid = actions.find((a) => a.type === "Bid");
  if (bid) {
    const passBid = actions.find((a) => a.type === "PassBid");
    const min = Number(bid.payload?.minBid || 1);
    const max = Number(bid.payload?.maxBid || min);
    box.appendChild(contextCard("竞拍港务长", `
      <div class="bid-input-row">
        <input id="bidAmountInput" type="number" min="${min}" max="${max}" value="${min}" />
      </div>
      <div class="empty-note bid-range-note">允许范围 ${min} 到 ${max}。</div>
      <div class="inline-controls bid-button-row">
        <button id="bidSubmitBtn" type="button">出价</button>
        ${passBid ? `<button id="bidPassBtn" type="button">放弃</button>` : ""}
      </div>
    `));
    $("bidAmountInput").onchange = () => clampNumberInput($("bidAmountInput"), min, max);
    $("bidSubmitBtn").onclick = () => submitBid(bid).catch(showError);
    if (passBid) $("bidPassBtn").onclick = () => submitAction(passBid).catch(showError);
  }

  if (state.phase === "HarborMasterSelectGoods") {
    box.appendChild(contextCard("选择出航货物", `
      <div class="empty-note">在上方货物牌中点选 3 种。</div>
      <div class="context-actions">
        <button id="selectGoodsSubmitBtn" type="button" ${selectedGoods.size === 3 ? "" : "disabled"}>确认出航</button>
      </div>
    `));
    $("selectGoodsSubmitBtn").onclick = () => submitSelectedGoods().catch(showError);
  }

  if (state.phase === "HarborMasterSetShips") {
    const ids = selectedShipIds();
    const controls = ids.map((id) => `
      <label>${goodsName(id)}
        <input class="ship-start-input" data-ship-id="${id}" type="number" min="0" max="5" value="${shipStarts[id] ?? 3}" />
      </label>
    `).join("");
    box.appendChild(contextCard("设置起点", `
      <div class="input-grid">${controls}</div>
      <div class="inline-controls action-row">
        <button id="startsSubmitBtn" type="button">确认起点</button>
        <span id="startsSumText" class="empty-note"></span>
      </div>
    `));
    box.querySelectorAll(".ship-start-input").forEach((input) => {
      input.oninput = () => {
        shipStarts[input.dataset.shipId] = Number(input.value);
        updateStartsSum();
        renderSea();
      };
      input.onchange = () => {
        const value = clampNumberInput(input, 0, 5);
        shipStarts[input.dataset.shipId] = value;
        updateStartsSum();
        renderSea();
      };
    });
    $("startsSubmitBtn").onclick = () => submitShipStarts().catch(showError);
    updateStartsSum();
  }

  if (state.phase === "NavigatorAction") {
    const moveAction = actions.find((a) => a.type === "NavigatorMove");
    const step = state.round?.navigatorStep === "big" ? "大领航员" : "小领航员";
    const ids = selectedShipIds();
    const controls = ids.map((id) => {
      const ship = getShip(id);
      const locked = ship?.status !== "sailing";
      if (locked) navigatorMoves[id] = 0;
      return `
      <label>${goodsName(id)}
        <input class="nav-move-input" data-ship-id="${id}" type="number" min="-2" max="2" value="${locked ? 0 : (navigatorMoves[id] || 0)}" ${locked ? "disabled" : ""} />
      </label>
    `;
    }).join("");
    box.appendChild(contextCard(step, `
      <div class="input-grid">${controls}</div>
      <div class="inline-controls action-row">
        <button id="navSubmitBtn" type="button" ${moveAction ? "" : "disabled"}>移动</button>
        <span id="navMoveHint" class="empty-note">${state.round?.navigatorStep === "big" ? "总移动量不超过 2；全 0 表示不移动。" : "总移动量不超过 1；全 0 表示不移动。"}</span>
      </div>
    `));
    box.querySelectorAll(".nav-move-input").forEach((input) => {
      input.oninput = () => {
        navigatorMoves[input.dataset.shipId] = parseNavigatorInputValue(input.value);
        updateNavigatorMoveState();
      };
      input.onchange = () => {
        const value = clampNumberInput(input, -2, 2);
        navigatorMoves[input.dataset.shipId] = value;
        updateNavigatorMoveState();
      };
    });
    $("navSubmitBtn").onclick = () => submitNavigatorMove(moveAction).catch(showError);
    updateNavigatorMoveState();
  }
}

function contextCard(title, html) {
  const el = document.createElement("div");
  el.className = "context-card";
  el.innerHTML = `<h3>${title}</h3>${html}`;
  return el;
}

function renderActionList() {
  const list = $("actionsList");
  if (!state) {
    list.innerHTML = `<div class="empty-note">尚无游戏。</div>`;
    return;
  }
  if (state.status === "ended") {
    list.innerHTML = `<div class="empty-note">游戏结束。</div>`;
    return;
  }
  if (actions.length === 0) {
    list.innerHTML = `<div class="empty-note">等待其他玩家，或当前阶段没有可选动作。</div>`;
    return;
  }
  if (state.phase === "Auction" && actions.some((action) => action.type === "Bid")) {
    list.innerHTML = "";
    return;
  }
  if (state.phase === "HarborMasterSelectGoods" && actions.some((action) => action.type === "SelectGoods")) {
    list.innerHTML = "";
    return;
  }
  if (state.phase === "HarborMasterSetShips" && actions.some((action) => action.type === "SetShipStarts")) {
    list.innerHTML = "";
    return;
  }
  if (state.phase === "NavigatorAction" && actions.some((action) => action.type === "NavigatorMove")) {
    list.innerHTML = "";
    return;
  }
  list.innerHTML = actions.map((action) => {
    return `<button class="action-btn" type="button" data-action-key="${escapeAttr(actionKeyFor(action))}">${actionIconHTML(action)}<span class="action-label-text">${actionLabel(action)}</span></button>`;
  }).join("");
  list.querySelectorAll(".action-btn").forEach((button) => {
    const action = actionByKey(button.dataset.actionKey);
    button.onclick = () => handleListedAction(action).catch(showError);
  });
}

async function handleListedAction(action) {
  if (!action) return;
  if (action.type === "Bid") {
    await promptBid(action);
    return;
  }
  if (action.type === "SelectGoods") {
    if (selectedGoods.size !== 3) {
      showToast("请先在货物牌上选 3 种。");
      return;
    }
    await submitSelectedGoods();
    return;
  }
  if (action.type === "SetShipStarts") {
    await submitShipStarts();
    return;
  }
  if (action.type === "NavigatorMove") {
    await submitNavigatorMove(action);
    return;
  }
  await submitAction(action);
}

function renderEvents() {
  const events = state.events || [];
  $("eventsList").innerHTML = events.map((event) => {
    return `<div class="event">#${event.seq} ${eventName(event.type)} ${formatEventData(event)}</div>`;
  }).join("");
}

async function submitBid(action) {
  const min = Number(action.payload?.minBid || 1);
  const max = Number(action.payload?.maxBid || min);
  const input = $("bidAmountInput");
  const amount = clampIntegerValue(input?.value, min, max);
  if (input) input.value = String(amount);
  await submitAction(action, { amount });
}

async function promptBid(action) {
  const min = Number(action.payload?.minBid || 1);
  const max = Number(action.payload?.maxBid || min);
  const value = window.prompt(`请输入出价，范围 ${min}-${max}`, String(min));
  if (value === null) return;
  const amount = clampIntegerValue(value, min, max);
  await submitAction(action, { amount });
}

function clampNumberInput(input, min, max) {
  const amount = clampIntegerValue(input?.value, min, max);
  if (input) input.value = String(amount);
  return amount;
}

function clampIntegerValue(value, min, max) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return min;
  const amount = Math.trunc(parsed);
  if (amount < min) return min;
  if (amount > max) return max;
  return amount;
}

async function submitSelectedGoods() {
  const action = actions.find((a) => a.type === "SelectGoods");
  if (!action) return;
  const goodsIds = Array.from(selectedGoods).sort((a, b) => a - b);
  if (goodsIds.length !== 3) throw new Error("必须选择 3 种货物。");
  await submitAction(action, { goodsIds });
}

async function submitShipStarts() {
  const action = actions.find((a) => a.type === "SetShipStarts");
  if (!action) return;
  const ids = selectedShipIds();
  const starts = {};
  let sum = 0;
  for (const id of ids) {
    const value = Number(shipStarts[id]);
    if (!Number.isInteger(value) || value < 0 || value > 5) {
      throw new Error("每艘船的起点必须是 0 到 5。");
    }
    starts[String(id)] = value;
    sum += value;
  }
  if (sum !== 9) throw new Error(`三艘船起点总和必须为 9，当前为 ${sum}。`);
  await submitAction(action, { starts });
}

async function submitNavigatorMove(action) {
  if (!action) return;
  document.querySelectorAll(".nav-move-input").forEach((input) => {
    const value = clampNavigatorInput(input);
    navigatorMoves[input.dataset.shipId] = value;
  });
  const moves = selectedShipIds()
    .filter((shipId) => getShip(shipId)?.status === "sailing")
    .map((shipId) => ({ shipId: Number(shipId), delta: parseNavigatorInputValue(navigatorMoves[shipId]) }));
  const activeMoves = moves.filter((move) => move.delta !== 0);
  if (state.round?.navigatorStep === "small") {
    const total = activeMoves.reduce((sum, move) => sum + Math.abs(move.delta), 0);
    if (total > 1 || activeMoves.some((move) => Math.abs(move.delta) > 1)) {
      throw new Error("小领航员总移动量不能超过 1。");
    }
  }
  if (state.round?.navigatorStep === "big") {
    const total = activeMoves.reduce((sum, move) => sum + Math.abs(move.delta), 0);
    if (total > 2 || activeMoves.some((move) => Math.abs(move.delta) > 2)) {
      throw new Error("大领航员总移动量不能超过 2。");
    }
  }
  await submitAction(action, { moves });
}

function parseNavigatorInputValue(value) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return 0;
  return Math.trunc(parsed);
}

function clampNavigatorInput(input) {
  if (!input) return 0;
  const parsed = Number(input.value);
  let value = Number.isFinite(parsed) ? Math.trunc(parsed) : 0;
  if (value < -2) value = -2;
  if (value > 2) value = 2;
  input.value = String(value);
  return value;
}

function updateStartsSum() {
  const ids = selectedShipIds();
  const values = ids.map((id) => Number(shipStarts[id]));
  const sum = values.reduce((total, value) => total + (Number.isFinite(value) ? value : 0), 0);
  const validValues = values.every((value) => Number.isInteger(value) && value >= 0 && value <= 5);
  const valid = validValues && sum === 9;
  const el = $("startsSumText");
  if (el) {
    el.textContent = validValues ? `当前总和 ${sum} / 9` : "请输入 0-5 的整数";
    el.classList.toggle("invalid", !valid);
  }
  const button = $("startsSubmitBtn");
  if (button) button.disabled = !valid;
}

function updateNavigatorMoveState() {
  const button = $("navSubmitBtn");
  const hint = $("navMoveHint");
  if (!button || !hint) return;
  const moves = selectedShipIds()
    .map((shipId) => ({ shipId: Number(shipId), delta: parseNavigatorInputValue(navigatorMoves[shipId]) }))
    .filter((move) => getShip(move.shipId)?.status === "sailing")
    .filter((move) => move.delta !== 0);
  let valid = true;
  let text = state.round?.navigatorStep === "big" ? "总移动量不超过 2；全 0 表示不移动。" : "总移动量不超过 1；全 0 表示不移动。";
  if (state.round?.navigatorStep === "small") {
    const total = moves.reduce((sum, move) => sum + Math.abs(move.delta), 0);
    valid = total <= 1 && moves.every((move) => Math.abs(move.delta) <= 1);
    if (!valid) text = `总移动量 ${total} / 1，超出上限。`;
  } else if (state.round?.navigatorStep === "big") {
    const total = moves.reduce((sum, move) => sum + Math.abs(move.delta), 0);
    valid = total <= 2 && moves.every((move) => Math.abs(move.delta) <= 2);
    if (!valid) text = `总移动量 ${total} / 2，超出上限。`;
  }
  button.disabled = !valid;
  hint.textContent = text;
  hint.classList.toggle("invalid", !valid);
}

function syncTransientControls() {
  if (!state) return;
  if (state.phase === "HarborMasterSelectGoods" && selectedGoods.size === 0) {
    selectedGoods = new Set(selectedShipIds().slice(0, 3));
  }
  if (state.phase !== "HarborMasterSelectGoods") {
    selectedGoods = new Set();
  }
  if (state.phase === "HarborMasterSetShips") {
    const ids = selectedShipIds();
    if (Object.keys(shipStarts).length === 0 || ids.some((id) => shipStarts[id] === undefined)) {
      shipStarts = defaultStarts(ids);
    }
  } else {
    shipStarts = {};
  }
  if (state.phase !== "NavigatorAction") {
    navigatorMoves = {};
  }
}

function resetTransientControls() {
  selectedGoods = new Set();
  shipStarts = {};
  navigatorMoves = {};
}

function defaultStarts(ids) {
  const out = {};
  let remaining = 9;
  ids.forEach((id, index) => {
    const left = ids.length - index;
    const value = index === ids.length - 1 ? remaining : Math.min(5, Math.max(0, Math.floor(remaining / left)));
    out[id] = value;
    remaining -= value;
  });
  return out;
}

function placementAction(positionType, targetId) {
  return actions.find((a) => {
    if (a.type !== "PlaceAccomplice") return false;
    const payload = a.payload || {};
    return payload.positionType === positionType && String(payload.targetId) === String(targetId);
  });
}

function destinationAction(destination) {
  return actions.find((a) => {
    return a.type === "PirateChooseDestination"
      && String(a.payload?.destination) === destination;
  });
}

function actionByKey(key) {
  return actions.find((a) => actionKeyFor(a) === key);
}

function actionKeyFor(action) {
  return `${action.type}:${stableStringify(normalizePayload(action.payload || {}))}`;
}

function stableStringify(value) {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

function normalizePayload(payload) {
  const out = { ...payload };
  for (const key of ["goodsId", "shipId", "targetId"]) {
    if (out[key] !== undefined && typeof out[key] === "string" && /^\d+$/.test(out[key])) out[key] = Number(out[key]);
  }
  return out;
}

function getGoods(id) {
  return state.goods?.[id] || state.goods?.[String(id)];
}

function getShip(id) {
  return state.ships?.[id] || state.ships?.[String(id)];
}

function selectedShipIds() {
  const fromRound = (state.round?.selectedGoods || []).map(Number);
  if (fromRound.length) return fromRound;
  return Object.values(state.ships || {}).map((ship) => Number(ship.shipId)).sort((a, b) => a - b);
}

function sailingShips() {
  return Object.values(state.ships || {}).filter((ship) => ship.status === "sailing");
}

function shipBySlot(kind, slotId) {
  const key = kind === "port" ? "portSlot" : "dockSlot";
  return Object.values(state.ships || {}).find((ship) => ship[key] === slotId);
}

function pirateSlots() {
  const active = [...(state.board?.pirates || [])];
  const boarded = state.board?.boardedPirates || [];
  const slots = [null, null];
  const boardedCaptain = boarded.find((p) => p.role === "boardedCaptain");
  const boardedMate = boarded.find((p) => p.role === "boardedMate");
  if (boardedCaptain) slots[0] = boardedCaptain;
  if (boardedMate) slots[1] = boardedMate;
  for (const pirate of active) {
    if (!slots[0]) slots[0] = pirate;
    else if (!slots[1]) slots[1] = pirate;
  }
  return slots;
}

function boardingCosts(id) {
  const goods = getGoods(id);
  if (goods?.boardingCosts) return goods.boardingCosts.map(Number);
  return goodsMeta[id]?.costs || [];
}

function shareSummary(player) {
  if (Number(player.playerId) === getLocalPlayerId()) {
    const shares = goodsOrder.map((id) => `${goodsName(id)} ${Number(player.shares?.[id] || 0)}`).join(" · ");
    return `<span class="share-goods-row">${shares}</span><span class="share-meta-row">抵押 ${mortgageTotal(player)}</span>`;
  }
  const known = knownBoughtShares(player.playerId);
  const knownText = goodsOrder.map((id) => `${goodsName(id)} ${Number(known[id] || 0)}`).join(" · ");
  return `<span class="share-goods-row">${knownText}</span><span class="share-meta-row">未知 ${hiddenShareCount(player)} · 抵押 ${mortgageTotal(player)}</span>`;
}

function knownBoughtShares(playerId) {
  const out = {};
  for (const event of state.events || []) {
    if (event.type !== "ShareBought") continue;
    if (Number(event.data?.playerId) !== Number(playerId)) continue;
    const id = Number(event.data.goodsId);
    out[id] = (out[id] || 0) + 1;
  }
  return out;
}

function hiddenShareCount(player) {
  if (player && player.hiddenShareCount !== undefined) return Number(player.hiddenShareCount || 0);
  const total = Object.values(player.shares || {}).reduce((sum, value) => sum + Number(value || 0), 0);
  const known = Object.values(knownBoughtShares(player.playerId)).reduce((sum, value) => sum + Number(value || 0), 0);
  return Math.max(0, total - known);
}

function mortgageTotal(player) {
  return Number(player?.mortgagedShareCount || 0);
}

function actionLabel(action) {
  const payload = action.payload || {};
  const cost = action.cost ? ` · 花费 ${action.cost}` : "";
  switch (action.type) {
    case "Bid": return `出价 ${payload.minBid}-${payload.maxBid}`;
    case "PassBid": return "竞拍：放弃";
    case "BuyShare": return `购买 ${goodsName(payload.goodsId)} 股份${cost}`;
    case "SkipBuyShare": return "跳过购买股份";
    case "SelectGoods": return "确认出航货物";
    case "SetShipStarts": return "确认货船起点";
    case "PlaceAccomplice": return placementLabel(payload, cost);
    case "PirateBoard": return `海盗登上 ${goodsName(payload.shipId)} 船`;
    case "PirateSkipBoard": return "海盗留在船上";
    case "NavigatorMove": return "领航员移动货船";
    case "PirateChooseDestination": return `把 ${goodsName(payload.shipId)} 船送往${payload.destination === "port" ? "港口" : "船坞"}`;
    case "ConfirmRound": return "确认本轮结算";
    default: return action.type;
  }
}

function actionIconHTML(action) {
  const src = actionIconSrc(action);
  if (!src) return `<span class="action-icon-placeholder" aria-hidden="true"></span>`;
  return `<img class="action-icon" src="${src}" alt="" aria-hidden="true" loading="lazy">`;
}

function actionIconSrc(action) {
  const payload = action.payload || {};
  if (action.type === "BuyShare") return goodsIconSrc(payload.goodsId);
  if (action.type === "SelectGoods") return goodsIconSrc((payload.goodsIds || [])[0]);
  if (action.type === "PlaceAccomplice") return placementIconSrc(payload);
  if (action.type === "PirateBoard" || action.type === "PirateSkipBoard") return "assets/board-icons/icon-pirates.png";
  if (action.type === "NavigatorMove") return "assets/board-icons/icon-pilots.png";
  if (action.type === "PirateChooseDestination") {
    return payload.destination === "dock" ? "assets/board-icons/icon-shipyard.png" : "assets/board-icons/icon-port.png";
  }
  return "";
}

function placementIconSrc(payload) {
  if (payload.positionType === "ship") return goodsIconSrc(payload.targetId);
  if (payload.positionType === "port") return "assets/board-icons/icon-port.png";
  if (payload.positionType === "dock") return "assets/board-icons/icon-shipyard.png";
  if (payload.positionType === "pirate") return "assets/board-icons/icon-pirates.png";
  if (payload.positionType === "navigatorSmall" || payload.positionType === "navigatorBig") return "assets/board-icons/icon-pilots.png";
  if (payload.positionType === "insurance") return "assets/board-icons/icon-insurance.png";
  return "";
}

function goodsIconSrc(goodsId) {
  switch (Number(goodsId)) {
    case 1: return "assets/goods-icons/goods-ginseng.png";
    case 2: return "assets/goods-icons/goods-nutmeg.png";
    case 3: return "assets/goods-icons/goods-silk.png";
    case 4: return "assets/goods-icons/goods-jade.png";
    default: return "";
  }
}

function placementLabel(payload, cost) {
  if (payload.positionType === "ship") return `${payload.isStowaway ? "偷渡" : "上船"} ${goodsName(payload.targetId)}${cost}`;
  if (payload.positionType === "port") return `港口 ${payload.targetId}${cost}`;
  if (payload.positionType === "dock") return `船坞 ${payload.targetId}${cost}`;
  if (payload.positionType === "pirate") return `海盗船 ${Number(payload.targetId) === 1 ? "船长" : "副手"}${cost}`;
  if (payload.positionType === "navigatorSmall") return `领航员 小${cost}`;
  if (payload.positionType === "navigatorBig") return `领航员 大${cost}`;
  if (payload.positionType === "insurance") return `保险商${cost}`;
  return `${payload.positionType}${cost}`;
}

function phaseName(game) {
  if (game.phase === "Placement") return `放置同伙 ${game.round?.placementStep || 1}`;
  return {
    NotStarted: "未开始",
    Auction: "竞拍港务长",
    HarborMasterBuyShare: "港务长买股",
    HarborMasterSelectGoods: "选择出航货物",
    HarborMasterSetShips: "设置货船起点",
    PirateBoarding: "海盗登船",
    NavigatorAction: "领航员行动",
    PirateLooting: "海盗劫掠",
    RoundReview: "本轮确认",
    GameEnd: "游戏结束",
  }[game.phase] || game.phase;
}

function eventName(type) {
  return {
    GameStarted: "游戏开始",
    RoundStarted: "新轮开始",
    BidPlaced: "玩家出价",
    BidPassed: "放弃竞拍",
    HarborMasterSet: "港务确定",
    PlayerPaid: "玩家支付",
    ShareMortgaged: "自动抵押",
    ShareBought: "股份购入",
    ShareBuySkipped: "放弃买股",
    GoodsSelected: "货物出航",
    ShipStartsSet: "起点设置",
    PlayerBecameBankrupt: "玩家破产",
    AccomplicePlaced: "同伙放置",
    DiceRolled: "掷骰移动",
    ShipArrived: "货船到港",
    ShipDocked: "货船进坞",
    PirateBoarded: "海盗登船",
    PirateBoardSkipped: "放弃登船",
    NavigatorMoved: "领航移动",
    NavigatorSkipped: "放弃领航",
    PiratesLootedShip: "海盗劫掠",
    PlayerReceivedPayout: "玩家收益",
    StowawayPayoutReceived: "偷渡收益",
    InsurancePaidDockReward: "保险赔付",
    BankCoveredInsuranceShortfall: "银行补款",
    GoodsMarketAdvanced: "货物升值",
    RoundReviewStarted: "结算完成",
    RoundConfirmed: "玩家确认",
    GameEnded: "游戏结束",
    RuleError: "规则异常",
  }[type] || type;
}

function formatEventData(event) {
  const data = event.data || {};
  switch (event.type) {
    case "GameStarted": return "游戏已开始";
    case "RoundStarted": return `第 ${data.roundNumber} 轮，P${data.startPlayer} 开始竞拍`;
    case "BidPlaced": return `P${data.playerId} 出价 ${data.amount}${hasValue(data.previousBid) ? `，上一价 ${data.previousBid}` : ""}`;
    case "BidPassed": return `P${data.playerId} 放弃竞拍${hasValue(data.currentBid) ? `，当前最高价 ${data.currentBid}${data.highestBidder ? `（P${data.highestBidder}）` : ""}` : ""}`;
    case "HarborMasterSet": return `${hasValue(data.roundNumber) ? `第 ${data.roundNumber} 轮，` : ""}P${data.playerId} 担任港务长，成交价 ${data.price}`;
    case "PlayerPaid": return `P${data.playerId} 支付 ${data.amount}，原因：${reasonName(data.reason)}${hasValue(data.cashAfter) ? `，剩余现金 ${data.cashAfter}` : ""}`;
    case "ShareMortgaged": return `P${data.playerId} 自动抵押 1 张，获得 ${data.amount}，当前抵押 ${data.mortgagedShareCount} 张${hasValue(data.cashAfter) ? `，现金 ${data.cashAfter}` : ""}`;
    case "ShareBought": return `P${data.playerId} 买入 ${goodsName(data.goodsId)}，价格 ${data.price}${hasValue(data.playerShareCount) ? `，自己持有 ${data.playerShareCount} 张` : ""}${hasValue(data.sharesRemaining) ? `，牌堆剩 ${data.sharesRemaining}` : ""}`;
    case "ShareBuySkipped": return `P${data.playerId} 未购买股份`;
    case "GoodsSelected": return `出航 ${goodsList(data.goodsIds)}，未出航 ${goodsName(data.excludedGoodsId)}`;
    case "ShipStartsSet": return `起点 ${formatStarts(data.starts)}`;
    case "PlayerBecameBankrupt": return `P${data.playerId} 进入破产状态${hasValue(data.cash) ? `，现金 ${data.cash}` : ""}${hasValue(data.minCost) ? `，最低可行动作费用 ${data.minCost}` : ""}`;
    case "AccomplicePlaced": return formatPlacementEvent(data);
    case "DiceRolled": return `第 ${data.movementStep} 次移动：${formatDiceResults(data.results)}${hasEntries(data.positionsAfter) ? `；船位 ${formatPositions(data.positionsAfter)}` : ""}`;
    case "ShipArrived": return `${goodsName(data.shipId)} 到港 ${data.slot}${hasValue(data.position) ? `，船位 ${data.position}` : ""}`;
    case "ShipDocked": return `${goodsName(data.shipId)} 进船坞 ${data.slot}${hasValue(data.position) ? `，船位 ${data.position}` : ""}`;
    case "PirateBoarded": return `P${data.playerId}${data.role ? `（${roleName(data.role)}）` : ""} 登上 ${goodsName(data.shipId)}${data.shipRole ? `，货船身份：${roleName(data.shipRole)}` : ""}`;
    case "PirateBoardSkipped": return `P${data.playerId}${data.role ? `（${roleName(data.role)}）` : ""} 放弃登船`;
    case "NavigatorMoved": return `P${data.playerId} ${navigatorName(data.navigator)}：${formatMoves(data.moves)}${hasEntries(data.positionsAfter) ? `；船位 ${formatPositions(data.positionsAfter)}` : ""}`;
    case "NavigatorSkipped": return `P${data.playerId} ${navigatorName(data.navigator)} 未移动`;
    case "PiratesLootedShip": return `${data.playerId ? `P${data.playerId} 将 ` : ""}${goodsName(data.shipId)} 送往${destinationName(data.destination)}`;
    case "PlayerReceivedPayout": return `P${data.playerId} 获得 ${data.amount}，来源：${payoutDetail(data)}`;
    case "StowawayPayoutReceived": return `P${data.playerId} 从 ${goodsName(data.shipId)} 偷渡收益获得 ${data.amount}${hasValue(data.normalShareCount) ? `，普通分账人数 ${data.normalShareCount}` : ""}`;
    case "InsurancePaidDockReward": return `P${data.playerId} 赔付船坞收益 ${data.amount}${data.slot ? `，船坞 ${data.slot}` : ""}${hasValue(data.required) ? `，应赔 ${data.required}` : ""}`;
    case "BankCoveredInsuranceShortfall": return `P${data.playerId} 现金不足，银行补 ${data.amount}${data.slot ? `，船坞 ${data.slot}` : ""}${hasValue(data.paid) && hasValue(data.required) ? `，已付 ${data.paid}/${data.required}` : ""}`;
    case "GoodsMarketAdvanced": return `${goodsName(data.goodsId)} ${hasValue(data.oldMarketValue) ? `股价 ${data.oldMarketValue} -> ${data.marketValue}` : `升至 ${data.marketValue}`}`;
    case "RoundReviewStarted": return `第 ${data.roundNumber} 轮结算完成，等待玩家确认`;
    case "RoundConfirmed": return `P${data.playerId} 已确认第 ${data.roundNumber} 轮`;
    case "GameEnded": return formatScores(data.scores);
    case "RuleError": return Object.keys(data).length ? `${event.message || "规则异常"}：${JSON.stringify(data)}` : (event.message || "规则异常");
    default: return Object.keys(data).length ? JSON.stringify(data) : (event.message || "");
  }
}

function formatPlacementEvent(data) {
  const cost = data.cost !== undefined ? `，花费 ${data.cost}` : "";
  if (data.positionType === "ship") return `P${data.playerId} ${data.stowaway ? "偷渡" : "上船"} ${goodsName(data.shipId)}${data.boardingSlot ? ` 第 ${data.boardingSlot} 格` : ""}${cost}`;
  if (data.positionType === "port") return `P${data.playerId} 押港口 ${data.slot}${cost}${hasValue(data.reward) ? `，可得 ${data.reward}` : ""}`;
  if (data.positionType === "dock") return `P${data.playerId} 押船坞 ${data.slot}${cost}${hasValue(data.reward) ? `，可得 ${data.reward}` : ""}`;
  if (data.positionType === "pirate") return `P${data.playerId} 上海盗船${data.role ? ` ${roleName(data.role)}位` : ""}${cost}`;
  if (data.positionType === "navigatorSmall") return `P${data.playerId} 放置小领航员${cost}`;
  if (data.positionType === "navigatorBig") return `P${data.playerId} 放置大领航员${cost}`;
  if (data.positionType === "insurance") return `P${data.playerId} 成为保险商，花费 ${data.cost ?? 0}，立即获得 ${data.reward}`;
  return `P${data.playerId} 放置到 ${data.positionType}${cost}`;
}

function goodsList(ids) {
  return (ids || []).map(goodsName).join("、");
}

function formatStarts(starts) {
  if (!starts) return "";
  return Object.entries(starts).sort(([a], [b]) => Number(a) - Number(b)).map(([id, value]) => `${goodsName(id)}:${value}`).join(" ");
}

function formatDiceResults(results) {
  if (!results) return "无";
  return Object.entries(results).sort(([a], [b]) => Number(a) - Number(b)).map(([id, value]) => `${goodsName(id)}+${value}`).join(" ");
}

function formatPositions(positions) {
  if (!positions || Object.keys(positions).length === 0) return "无";
  return Object.entries(positions).sort(([a], [b]) => Number(a) - Number(b)).map(([id, value]) => `${goodsName(id)}=${value}`).join(" ");
}

function formatMoves(moves) {
  if (!moves || moves.length === 0) return "无移动";
  return moves.map((move) => `${goodsName(move.shipId)} ${Number(move.delta) > 0 ? "+" : ""}${move.delta}`).join("，");
}

function formatScores(scores) {
  if (!scores || scores.length === 0) return "";
  return scores.map((score) => `第${score.rank}名 P${score.playerId} ${score.wealth}分`).join("；");
}

function reasonName(reason) {
  return {
    "harbor master bid": "竞拍港务长",
    "buy share": "购买股份",
    "place accomplice": "放置同伙",
  }[reason] || reason || "未知";
}

function sourceName(source) {
  return {
    pirates: "海盗分赃",
    ship: "货船分账",
    port: "港口押注",
    dock: "船坞押注",
  }[source] || source || "未知";
}

function destinationName(destination) {
  return {
    port: "港口",
    dock: "船坞",
  }[destination] || destination || "未知";
}

function navigatorName(name) {
  return {
    small: "小领航员",
    big: "大领航员",
  }[name] || name || "领航员";
}

function roleName(role) {
  return {
    captain: "船长",
    mate: "副手",
    boardedPirate: "海盗",
    boardedCaptain: "已登船",
    boardedMate: "已登船",
  }[role] || role || "无身份";
}

function payoutDetail(data) {
  if (data.source === "ship" || data.source === "pirates") {
    return `${sourceName(data.source)} ${goodsName(data.shipId)}${hasValue(data.shipPayout) ? `，总收益 ${data.shipPayout}` : ""}${hasValue(data.shareCount) ? `，分账人数 ${data.shareCount}` : ""}`;
  }
  if (data.source === "port" || data.source === "dock") {
    return `${sourceName(data.source)} ${data.slot}`;
  }
  return sourceName(data.source);
}

function hasValue(value) {
  return value !== undefined && value !== null && value !== "";
}

function hasEntries(value) {
  return value && Object.keys(value).length > 0;
}

function pieceHTML(piece) {
  if (!piece) return `<span class="piece empty">空</span>`;
  const boarded = piece.role === "boardedCaptain" || piece.role === "boardedMate";
  const label = boarded ? `P${piece.playerId}/已登船` : `P${piece.playerId}`;
  return `<span class="piece ${boarded ? "boarded" : ""}" style="${pieceStyle(piece)}">${label}</span>`;
}

function pieceStyle(piece) {
  if (!piece) return "";
  return `--player-color: ${playerColors[Number(piece.playerId)] || "#555"};`;
}

function pieceText(piece) {
  if (!piece) return "空";
  const role = {
    captain: "船长",
    mate: "副手",
    boardedPirate: "海盗",
    boardedCaptain: "已登船",
    boardedMate: "已登船",
  }[piece.role] || "";
  return `P${piece.playerId}${role ? `/${role}` : ""}`;
}

function goodsName(id) {
  return goodsMeta[Number(id)]?.name || id;
}

function shipStatusName(status) {
  return { sailing: "航行中", arrived: "已到港", docked: "进船坞" }[status] || status;
}

function setBusyButtons(isBusy) {
  $("aiStepBtn").disabled = isBusy;
  $("aiRoundBtn").disabled = isBusy;
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

function escapeAttr(value) {
  return String(value).replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;");
}

$("newGameBtn").onclick = () => createGame().catch(showError);
$("aiStepBtn").onclick = () => aiStep().catch(showError);
$("aiRoundBtn").onclick = () => aiAdvanceRound().catch(showError);

refresh().catch(() => renderEmpty());
