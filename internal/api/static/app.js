let gameId = localStorage.getItem("manilaGameId") || "";
let state = null;
let aiBusy = false;
let revealAllShares = localStorage.getItem("manilaRevealAllShares") === "true";

const $ = (id) => document.getElementById(id);
const ROUND_ADVANCE_MAX_STEPS = 600;

async function api(path, options = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.message || res.statusText);
  return data;
}

async function newGame() {
  const seedValue = $("seed").value.trim();
  const body = seedValue ? { seed: Number(seedValue) } : {};
  const created = await api("/debug/games", { method: "POST", body: JSON.stringify(body) });
  gameId = created.game.gameId;
  localStorage.setItem("manilaGameId", gameId);
  await api(`/debug/games/${gameId}/start`, { method: "POST" });
  await refresh();
  await maybeAutoAI();
}

async function refresh(options = {}) {
  if (!gameId) return renderEmpty();
  const data = await api(`/debug/games/${gameId}/state`);
  state = data.game;
  render();
  if (options.auto !== false) {
    setTimeout(() => maybeAutoAI().catch(showError), 120);
  }
}

async function legalActions() {
  if (!state || !state.currentPlayer) return [];
  const data = await api(`/debug/games/${gameId}/players/${state.currentPlayer}/actions`);
  return data.actions || [];
}

async function applyAction(action) {
  const actionPayload = preparePayloadForSubmit(action);
  if (actionPayload === null) return;
  const requestBody = {
    playerId: state.currentPlayer,
    type: action.type,
    payload: actionPayload,
    expectedEventSeq: state.eventSeq,
  };
  await api(`/debug/games/${gameId}/actions`, { method: "POST", body: JSON.stringify(requestBody) });
  await refresh();
}

async function aiStep() {
  if (aiBusy) return;
  if (!state || !state.currentPlayer) return;
  aiBusy = true;
  try {
    const playerId = Number(state.currentPlayer);
    await api(`/debug/games/${gameId}/players/${playerId}/trained-ai`, { method: "POST" });
    await refresh({ auto: false });
  } finally {
    aiBusy = false;
  }
  await maybeAutoAI();
}

async function aiStepInternal() {
  await api(`/debug/games/${gameId}/players/${state.currentPlayer}/trained-ai`, { method: "POST" });
  const data = await api(`/debug/games/${gameId}/state`);
  state = data.game;
  render();
}

async function maybeAutoAI() {
  if (!$("autoAI").checked || aiBusy || !state || state.status === "ended") return;
  if (![2, 3, 4].includes(Number(state.currentPlayer))) return;
  aiBusy = true;
  try {
    for (let i = 0; i < 80; i++) {
      if (!state || state.status === "ended" || ![2, 3, 4].includes(Number(state.currentPlayer))) break;
      await aiStepInternal();
    }
  } finally {
    aiBusy = false;
  }
}

async function aiAdvanceRound() {
  if (aiBusy) return;
  aiBusy = true;
  let reachedStopPoint = false;
  let hasLeftStartingReview = !state || state.phase !== "RoundReview";
  try {
    for (let i = 0; i < ROUND_ADVANCE_MAX_STEPS; i++) {
      if (!state || state.status === "ended" || !state.currentPlayer) {
        reachedStopPoint = true;
        break;
      }
      if (state.phase === "RoundReview" && hasLeftStartingReview) {
        reachedStopPoint = true;
        break;
      }
      await aiStepInternal();
      if (!state || state.status === "ended") {
        reachedStopPoint = true;
        break;
      }
      if (state.phase !== "RoundReview") {
        hasLeftStartingReview = true;
      }
    }
  } finally {
    aiBusy = false;
  }
  if (!reachedStopPoint && state && state.status !== "ended" && state.phase !== "RoundReview") {
    showError(new Error("AI推进一轮达到安全步数上限，请检查当前局面"));
  }
}

function preparePayloadForSubmit(action) {
  const payload = normalizePayload(action.payload || {});
  if (action.type === "Bid") {
    const min = Number(payload.minBid || 1);
    const max = Number(payload.maxBid || min);
    const value = prompt(`请输入竞价金额（${min} - ${max}）`, String(min));
    if (value === null) return null;
    const amount = Number(value);
    if (!Number.isInteger(amount) || amount < min || amount > max) {
      alert(`竞价必须是 ${min} 到 ${max} 之间的整数`);
      return null;
    }
    payload.amount = amount;
  }
  if (action.type === "SelectGoods") {
    const text = prompt("请选择本轮出航的3种货物ID，用逗号分隔：1=人参, 2=肉豆蔻, 3=丝绸, 4=玉石", "1,2,3");
    if (text === null) return null;
    const goodsIds = parseNumberList(text);
    const unique = new Set(goodsIds);
    if (goodsIds.length !== 3 || unique.size !== 3 || goodsIds.some((n) => n < 1 || n > 4)) {
      alert("必须输入3个不重复的货物ID，例如：1,2,3");
      return null;
    }
    payload.goodsIds = goodsIds;
  }
  if (action.type === "SetShipStarts") {
    const selected = (state.round && state.round.selectedGoods || []).map(Number);
    const fallback = (selected.length ? selected : [1, 2, 3]).sort((a, b) => a - b);
    const defaults = fallback.map((id) => defaultStarts(fallback)[id] ?? 3).join(",");
    const text = prompt(`按货物ID从小到大输入三艘船起点：${fallback.map((id) => goodsName(id)).join("、")}。总和必须为9。`, defaults);
    if (text === null) return null;
    const values = parseNumberList(text);
    if (values.length !== fallback.length || values.some((n) => n < 0 || n > 5)) {
      alert(`请输入${fallback.length}个0到5之间的整数，例如：${defaults}`);
      return null;
    }
    const starts = {};
    let sum = 0;
    fallback.forEach((shipId, index) => {
      starts[String(shipId)] = values[index];
      sum += values[index];
    });
    if (sum !== 9) {
      alert(`三艘船起点总和必须为9，当前是 ${sum}`);
      return null;
    }
    payload.starts = starts;
  }
  if (action.type === "NavigatorMove") {
    const step = state.round && state.round.navigatorStep;
    const hint = step === "big"
      ? "大领航员：输入 shipId:delta，可一艘最多±2，或两艘各±1。例如 1:2 或 1:1,2:-1"
      : "小领航员：输入 shipId:delta，只能移动一艘±1。例如 1:1 或 2:-1";
    const text = prompt(hint, "");
    if (text === null) return null;
    if (text.trim() === "") {
      payload.moves = [];
      return payload;
    }
    const moves = parseMoves(text);
    if (!moves) return null;
    payload.moves = moves;
  }
  return payload;
}

function normalizePayload(payload) {
  const out = { ...payload };
  if (out.goodsIds && Array.isArray(out.goodsIds)) out.goodsIds = out.goodsIds.map(Number);
  return out;
}

function parseNumberList(text) {
  return text.split(",").map((s) => Number(s.trim())).filter((n) => Number.isInteger(n));
}

function parseMoves(text) {
  const parts = text.split(",").map((s) => s.trim()).filter(Boolean);
  const moves = [];
  for (const part of parts) {
    const [shipRaw, deltaRaw] = part.split(":").map((s) => s && s.trim());
    const shipId = Number(shipRaw);
    const delta = Number(deltaRaw);
    if (!Number.isInteger(shipId) || shipId < 1 || shipId > 4 || !Number.isInteger(delta) || delta < -2 || delta > 2 || delta === 0) {
      alert("移动格式不正确。示例：1:1 或 1:1,2:-1");
      return null;
    }
    moves.push({ shipId, delta });
  }
  if (moves.length === 0) {
    alert("请输入移动，或留空表示不移动");
    return null;
  }
  return moves;
}

function defaultStarts(goodsIds) {
  const starts = {};
  let remaining = 9;
  goodsIds.forEach((id, index) => {
    const left = goodsIds.length - index;
    const value = index === goodsIds.length - 1 ? remaining : Math.min(5, Math.max(0, Math.floor(remaining / left)));
    starts[id] = value;
    remaining -= value;
  });
  return starts;
}

function renderEmpty() {
  $("gameId").textContent = "-";
  $("phase").textContent = "-";
  $("round").textContent = "-";
  $("currentPlayer").textContent = "-";
  $("players").innerHTML = "";
  $("goodsMarket").innerHTML = "";
  $("ships").innerHTML = "";
  $("boardPositions").innerHTML = "";
  $("actions").innerHTML = "请先新建游戏";
  $("events").innerHTML = "";
  $("raw").textContent = "{}";
}

async function render() {
  $("gameId").textContent = state.gameId;
  $("phase").textContent = phaseName(state);
  $("round").textContent = state.roundNumber;
  $("currentPlayer").textContent = state.currentPlayer || "-";
  renderPlayers();
  renderGoodsMarket();
  renderShips();
  renderBoardPositions();
  renderEvents();
  $("raw").textContent = JSON.stringify(state, null, 2);
  await renderActions();
}

function renderPlayers() {
  const players = Object.values(state.players || {}).sort((a, b) => a.playerId - b.playerId);
  $("toggleShares").textContent = revealAllShares ? "隐藏对手股份" : "显示全部股份";
  $("players").innerHTML = players.map((p) => {
    return `<div class="item"><b>玩家 ${p.playerId}</b> 现金 ${p.cash} 可用同伙 ${p.availableAccomplices}<br><span class="share-line">${shareSummary(p)}</span>${p.bankruptThisRound ? "<br>本轮破产" : ""}</div>`;
  }).join("");
}

function shareSummary(player) {
  if (player.playerId === 1 || revealAllShares) {
    const shares = Object.entries(player.shares || {}).map(([k, v]) => `${goodsName(k)}:${v}`).join(" ");
    return `股份 ${shares} 抵押:${mortgageTotal(player)}`;
  }
  const bought = knownBoughtShares(player.playerId);
  const visibleShares = Object.entries(bought).sort(([a], [b]) => Number(a) - Number(b)).map(([k, v]) => `${goodsName(k)}:${v}`);
  visibleShares.push(`未知:${hiddenShareCount(player)}`);
  visibleShares.push(`抵押:${mortgageTotal(player)}`);
  return `股份 ${visibleShares.join(" ")}`;
}

function knownBoughtShares(playerId) {
  const bought = {};
  for (const event of state.events || []) {
    if (event.type !== "ShareBought" || !event.data || Number(event.data.playerId) !== Number(playerId)) continue;
    const goodsId = Number(event.data.goodsId);
    bought[goodsId] = (bought[goodsId] || 0) + 1;
  }
  return bought;
}

function mortgageTotal(player) {
  return Number(player.mortgagedShareCount || 0);
}

function hiddenShareCount(player) {
  if (player && player.hiddenShareCount !== undefined) return Number(player.hiddenShareCount || 0);
  if (!player || !player.shares) return 2;
  const totalShares = Object.values(player.shares).reduce((sum, value) => sum + Number(value || 0), 0);
  const boughtCount = Object.values(knownBoughtShares(player.playerId)).reduce((sum, value) => sum + Number(value || 0), 0);
  return Math.max(0, totalShares - boughtCount);
}

function renderGoodsMarket() {
  const goods = Object.values(state.goods || {}).sort((a, b) => a.goodsId - b.goodsId);
  if (goods.length === 0) {
    $("goodsMarket").innerHTML = "<div class='item'>暂无货物信息</div>";
    return;
  }
  $("goodsMarket").innerHTML = goods.map((g) => {
    const price = marketValue(g.marketValueIndex);
    const nextPrice = marketValue(Math.min(Number(g.marketValueIndex) + 1, 4));
    const progress = `${Number(g.marketValueIndex)}/4`;
    const next = price === nextPrice ? "已满" : `下级 ${nextPrice}`;
    return `<div class="market-item">
      <div><strong>${goodsName(g.goodsId)}</strong><span>价格 ${price}</span></div>
      <div class="market-meta">进度 ${progress} · ${next} · 剩余股份 ${g.sharesRemaining} · 船收益 ${g.shipPayout}</div>
    </div>`;
  }).join("");
}

function renderShips() {
  const ships = Object.values(state.ships || {}).sort((a, b) => a.shipId - b.shipId);
  if (ships.length === 0) {
    $("ships").innerHTML = "<div class='item'>尚未出航</div>";
    return;
  }
  $("ships").innerHTML = ships.map((s) => {
    const costs = boardingCosts(s.shipId);
    const seats = costs.map((cost, index) => {
      const occupant = (s.occupants || [])[index];
      return slotHTML(`座位${index + 1}`, `费${cost}`, occupant);
    }).join("");
    const stowaways = (s.stowaways || []).length
      ? `<div class="slot occupied"><span>偷渡</span><strong>${pieceList(s.stowaways)}</strong></div>`
      : "";
    return `<div class="item"><b>${goodsName(s.shipId)}</b> 位置 ${s.position} 状态 ${shipStatusName(s.status)}<br><div class="slot-list">${seats}${stowaways}</div></div>`;
  }).join("");
}

function renderBoardPositions() {
  const board = state.board || {};
  const sections = [
    renderBoardSection("港口", ["A", "B", "C"].map((id) => {
      const slot = board.ports && board.ports[id];
      return slotHTML(id, slot ? `费${slot.cost}/奖${slot.reward}` : "", slot && slot.occupant);
    })),
    renderBoardSection("船坞", ["A", "B", "C"].map((id) => {
      const slot = board.docks && board.docks[id];
      return slotHTML(id, slot ? `费${slot.cost}/奖${slot.reward}` : "", slot && slot.occupant);
    })),
    renderBoardSection("海盗船", pirateShipSlots(board)),
    renderBoardSection("领航员 / 保险", [
      slotHTML("小领航", "费2", board.smallNavigator),
      slotHTML("大领航", "费5", board.bigNavigator),
      slotHTML("保险", "得10", board.insurance),
    ]),
  ];
  $("boardPositions").innerHTML = sections.join("");
}

function renderBoardSection(title, slots) {
  return `<div class="board-section"><h3>${title}</h3><div class="slot-list">${slots.join("")}</div></div>`;
}

function slotHTML(label, meta, piece) {
  const occupied = Boolean(piece);
  return `<div class="slot ${occupied ? "occupied" : "empty"}"><span>${label}${meta ? ` · ${meta}` : ""}</span><strong>${occupied ? pieceText(piece) : "空"}</strong></div>`;
}

function pirateShipSlots(board) {
  const active = [...(board.pirates || [])];
  const boarded = board.boardedPirates || [];
  const captainMarker = boarded.find((p) => p.role === "boardedCaptain");
  const mateMarker = boarded.find((p) => p.role === "boardedMate");
  const slots = [null, null];

  if (captainMarker) slots[0] = captainMarker;
  if (mateMarker) slots[1] = mateMarker;

  for (const pirate of active) {
    if (!slots[0]) {
      slots[0] = pirate;
    } else if (!slots[1]) {
      slots[1] = pirate;
    }
  }

  return [
    slotHTML("船长", "费5", slots[0]),
    slotHTML("副手", "费5", slots[1]),
  ];
}

async function renderActions() {
  const el = $("actions");
  el.innerHTML = "";
  if (!state || state.status === "ended") {
    el.textContent = "游戏已结束";
    return;
  }
  const actions = await legalActions();
  if (actions.length === 0) {
    el.textContent = "等待其他玩家或无可用动作";
    return;
  }
  const hint = actionHint();
  if (hint) {
    const hintEl = document.createElement("div");
    hintEl.className = "action-hint";
    hintEl.textContent = hint;
    el.appendChild(hintEl);
  }
  for (const action of actions) {
    const btn = document.createElement("button");
    btn.textContent = actionLabel(action);
    btn.onclick = () => applyAction(action).catch(showError);
    el.appendChild(btn);
  }
}

function renderEvents() {
  const events = state.events || [];
  $("events").innerHTML = events.slice(-80).map((e) => {
    return `<div class="event">#${e.seq} ${eventName(e.type)} ${formatEventData(e)}</div>`;
  }).join("");
}

function actionLabel(a) {
  const p = a.payload || {};
  const cost = a.cost ? ` 花费${a.cost}` : "";
  switch (a.type) {
    case "Bid":
      return `竞价：出价 ${p.minBid}-${p.maxBid}`;
    case "PassBid":
      return "竞价：不出价";
    case "BuyShare":
      return `购买股份：${goodsName(p.goodsId)}${cost}`;
    case "SkipBuyShare":
      return "跳过购买股份";
    case "SelectGoods":
      return "选择本轮出航货物";
    case "SetShipStarts":
      return "设置三艘货船起点";
    case "PlaceAccomplice":
      return placementLabel(p, cost);
    case "PirateBoard":
      return `海盗登船：登上 ${goodsName(p.shipId)} 船`;
    case "PirateSkipBoard":
      return "海盗登船：留在海盗船";
    case "NavigatorMove":
      return "领航员：移动货船";
    case "NavigatorSkip":
      return "领航员：不移动";
    case "PirateChooseDestination":
      return `海盗劫掠：将 ${goodsName(p.shipId)} 船送往${destinationName(p.destination)}`;
    case "ConfirmRound":
      return "确认本轮结果";
    default:
      return `${actionTypeName(a.type)}${cost}`;
  }
}

function actionHint() {
  if (!state) return "";
  if (state.phase === "PirateLooting") {
    const shipId = state.round && state.round.pendingLootShips && state.round.pendingLootShips[0];
    if (shipId) return `海盗船长正在处理 ${goodsName(shipId)} 船：选择它进港升值，或进船坞不升值。`;
  }
  if (state.phase === "PirateBoarding") {
    return "海盗可以登上停在 13 格且仍有空位的货船；货船上显示为海盗，海盗船上保留原身份并标记已登船。";
  }
  if (state.phase === "NavigatorAction") {
    const step = state.round && state.round.navigatorStep;
    return step === "big" ? "大领航员总移动量不超过 2。" : "小领航员总移动量不超过 1。";
  }
  if (state.phase === "RoundReview") {
    return "本轮结算已完成，请确认收益、升值和事件日志。所有玩家确认后进入下一轮。";
  }
  return "";
}

function placementLabel(p, cost) {
  if (p.positionType === "ship") {
    return `${p.isStowaway ? "偷渡" : "上船"} ${goodsName(p.targetId)}${p.occupiesSlot === false ? "（不占座）" : ""}${cost}`;
  }
  if (p.positionType === "port") return `港口 ${p.targetId}${cost}`;
  if (p.positionType === "dock") return `船坞 ${p.targetId}${cost}`;
  if (p.positionType === "pirate") return `海盗船 ${Number(p.targetId) === 1 ? "船长" : "副手"}${cost}`;
  if (p.positionType === "navigatorSmall") return `领航员 小${cost}`;
  if (p.positionType === "navigatorBig") return `领航员 大${cost}`;
  if (p.positionType === "insurance") return `保险商${cost}`;
  return `${positionName(p.positionType)} ${p.targetId}${cost}`;
}

function actionTypeName(type) {
  return {
    Bid: "竞价",
    PassBid: "不出价",
    BuyShare: "购买股份",
    SkipBuyShare: "跳过购买股份",
    SelectGoods: "选择出航货物",
    SetShipStarts: "设置船起点",
    PlaceAccomplice: "放置同伙",
    PirateBoard: "海盗登船",
    PirateSkipBoard: "海盗留船",
    NavigatorMove: "领航员移动",
    NavigatorSkip: "领航员跳过",
    PirateChooseDestination: "海盗选择去向",
    ConfirmRound: "确认本轮结果",
  }[type] || type;
}

function positionName(type) {
  return {
    ship: "货船",
    port: "港口",
    dock: "船坞",
    pirate: "海盗船",
    navigatorSmall: "小领航员",
    navigatorBig: "大领航员",
    insurance: "保险",
  }[type] || type;
}

function destinationName(destination) {
  return {
    port: "港口",
    dock: "船坞",
  }[destination] || destination;
}

function phaseName(gameOrPhase) {
  const game = typeof gameOrPhase === "object" ? gameOrPhase : null;
  const phase = game ? game.phase : gameOrPhase;
  const round = game && game.round ? game.round : {};
  if (phase === "Placement") {
    return `放置同伙 ${Number(round.placementStep || 1)}`;
  }
  return {
    NotStarted: "未开始",
    Auction: "竞拍港务长",
    HarborMasterBuyShare: "港务长购买股份",
    HarborMasterSelectGoods: "港务长选择货物",
    HarborMasterSetShips: "港务长设置船起点",
    PirateBoarding: "海盗登船",
    NavigatorAction: "领航员行动",
    PirateLooting: "海盗劫掠",
    RoundReview: "本轮结果确认",
    GameEnd: "游戏结束",
  }[phase] || phase;
}

function shipStatusName(status) {
  return {
    sailing: "航行中",
    arrived: "已到港",
    docked: "进船坞",
  }[status] || status;
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
    case "GameStarted":
      return "游戏已开始";
    case "BidPlaced":
      return `P${data.playerId} 出价 ${data.amount}${hasValue(data.previousBid) ? `，上一价 ${data.previousBid}` : ""}`;
    case "RoundReviewStarted":
      return `第 ${data.roundNumber} 轮结算完成，等待玩家确认`;
    case "RoundConfirmed":
      return `P${data.playerId} 已确认第 ${data.roundNumber} 轮`;
    case "RoundStarted":
      return `第 ${data.roundNumber} 轮，P${data.startPlayer} 开始竞拍`;
    case "BidPassed":
      return `P${data.playerId} 放弃竞拍${hasValue(data.currentBid) ? `，当前最高价 ${data.currentBid}${data.highestBidder ? `（P${data.highestBidder}）` : ""}` : ""}`;
    case "HarborMasterSet":
      return `${hasValue(data.roundNumber) ? `第 ${data.roundNumber} 轮，` : ""}P${data.playerId} 担任港务长，成交价 ${data.price}`;
    case "PlayerPaid":
      return `P${data.playerId} 支付 ${data.amount}，原因：${reasonName(data.reason)}${hasValue(data.cashAfter) ? `，剩余现金 ${data.cashAfter}` : ""}`;
    case "ShareMortgaged":
      return `P${data.playerId} 自动抵押 1 张，获得 ${data.amount}，当前抵押 ${data.mortgagedShareCount} 张${hasValue(data.cashAfter) ? `，现金 ${data.cashAfter}` : ""}`;
    case "ShareBought":
      return `P${data.playerId} 买入 ${goodsName(data.goodsId)}，价格 ${data.price}${hasValue(data.playerShareCount) ? `，自己持有 ${data.playerShareCount} 张` : ""}${hasValue(data.sharesRemaining) ? `，牌堆剩 ${data.sharesRemaining}` : ""}`;
    case "ShareBuySkipped":
      return `P${data.playerId} 未购买股份`;
    case "GoodsSelected":
      return `出航 ${goodsList(data.goodsIds)}，未出航 ${goodsName(data.excludedGoodsId)}`;
    case "ShipStartsSet":
      return `起点 ${formatStarts(data.starts)}`;
    case "PlayerBecameBankrupt":
      return `P${data.playerId} 进入破产状态${hasValue(data.cash) ? `，现金 ${data.cash}` : ""}${hasValue(data.minCost) ? `，最低可行动作费用 ${data.minCost}` : ""}`;
    case "AccomplicePlaced":
      return formatPlacementEvent(data);
    case "DiceRolled":
      return `第 ${data.movementStep} 次移动：${formatDiceResults(data.results)}${hasEntries(data.positionsAfter) ? `；船位 ${formatPositions(data.positionsAfter)}` : ""}`;
    case "ShipArrived":
      return `${goodsName(data.shipId)} 到港 ${data.slot}${hasValue(data.position) ? `，船位 ${data.position}` : ""}`;
    case "ShipDocked":
      return `${goodsName(data.shipId)} 进船坞 ${data.slot}${hasValue(data.position) ? `，船位 ${data.position}` : ""}`;
    case "PirateBoarded":
      return `P${data.playerId}${data.role ? `（${roleName(data.role)}）` : ""} 登上 ${goodsName(data.shipId)}${data.shipRole ? `，货船身份：${roleName(data.shipRole)}` : ""}`;
    case "PirateBoardSkipped":
      return `P${data.playerId}${data.role ? `（${roleName(data.role)}）` : ""} 放弃登船`;
    case "NavigatorMoved":
      return `P${data.playerId} ${navigatorName(data.navigator)}：${formatMoves(data.moves)}${hasEntries(data.positionsAfter) ? `；船位 ${formatPositions(data.positionsAfter)}` : ""}`;
    case "NavigatorSkipped":
      return `P${data.playerId} ${navigatorName(data.navigator)} 未移动`;
    case "PiratesLootedShip":
      return `${data.playerId ? `P${data.playerId} 将 ` : ""}${goodsName(data.shipId)} 送往${destinationName(data.destination)}`;
    case "PlayerReceivedPayout":
      return `P${data.playerId} 获得 ${data.amount}，来源：${payoutDetail(data)}`;
    case "StowawayPayoutReceived":
      return `P${data.playerId} 从 ${goodsName(data.shipId)} 偷渡收益获得 ${data.amount}${hasValue(data.normalShareCount) ? `，普通分账人数 ${data.normalShareCount}` : ""}`;
    case "InsurancePaidDockReward":
      return `P${data.playerId} 赔付船坞收益 ${data.amount}${data.slot ? `，船坞 ${data.slot}` : ""}${hasValue(data.required) ? `，应赔 ${data.required}` : ""}`;
    case "BankCoveredInsuranceShortfall":
      return `P${data.playerId} 现金不足，银行补 ${data.amount}${data.slot ? `，船坞 ${data.slot}` : ""}${hasValue(data.paid) && hasValue(data.required) ? `，已付 ${data.paid}/${data.required}` : ""}`;
    case "GoodsMarketAdvanced":
      return `${goodsName(data.goodsId)} ${hasValue(data.oldMarketValue) ? `股价 ${data.oldMarketValue} -> ${data.marketValue}` : `升至 ${data.marketValue}`}`;
    case "GameEnded":
      return formatScores(data.scores);
    case "RuleError":
      return Object.keys(data).length ? `${event.message || "规则异常"}：${JSON.stringify(data)}` : (event.message || "规则异常");
    default:
      return Object.keys(data).length ? JSON.stringify(data) : (event.message || "");
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
  return `P${data.playerId} 放置到 ${positionName(data.positionType)}${cost}`;
}

function goodsList(ids) {
  return (ids || []).map(goodsName).join("、");
}

function formatStarts(starts) {
  if (!starts) return "";
  return Object.entries(starts).sort(([a], [b]) => Number(a) - Number(b)).map(([shipId, value]) => `${goodsName(shipId)}:${value}`).join(" ");
}

function formatDiceResults(results) {
  if (!results) return "无";
  return Object.entries(results).sort(([a], [b]) => Number(a) - Number(b)).map(([shipId, value]) => `${goodsName(shipId)}+${value}`).join(" ");
}

function formatPositions(positions) {
  if (!positions || Object.keys(positions).length === 0) return "无";
  return Object.entries(positions).sort(([a], [b]) => Number(a) - Number(b)).map(([shipId, value]) => `${goodsName(shipId)}=${value}`).join(" ");
}

function formatMoves(moves) {
  if (!moves || moves.length === 0) return "无移动";
  return moves.map((m) => `${goodsName(m.shipId)} ${Number(m.delta) > 0 ? "+" : ""}${m.delta}`).join("，");
}

function formatScores(scores) {
  if (!scores || scores.length === 0) return "";
  return scores.map((s) => `第${s.rank}名 P${s.playerId} ${s.wealth}分`).join("；");
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

function navigatorName(name) {
  return {
    small: "小领航员",
    big: "大领航员",
  }[name] || name || "领航员";
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

function pieceList(list) {
  if (!list || list.length === 0) return "-";
  return list.map(pieceText).join(", ");
}

function pieceText(piece) {
  if (!piece) return "空";
  if (piece.role === "boardedCaptain" || piece.role === "boardedMate") {
    return `P${piece.playerId}/已登船`;
  }
  return `P${piece.playerId}${piece.role ? "/" + roleName(piece.role) : ""}`;
}

function roleName(role) {
  return {
    captain: "船长",
    mate: "副手",
    boardedPirate: "海盗",
    boardedCaptain: "已登船",
    boardedMate: "已登船",
    formerPirate: "海盗",
  }[role] || role;
}

function goodsName(id) {
  return { 1: "人参", 2: "肉豆蔻", 3: "丝绸", 4: "玉石" }[Number(id)] || id;
}

function marketValue(index) {
  return [0, 5, 10, 20, 30][Number(index)] ?? 0;
}

function boardingCosts(id) {
  const goods = state.goods && state.goods[Number(id)];
  if (goods && Array.isArray(goods.boardingCosts)) return goods.boardingCosts.map(Number);
  return { 1: [1, 2, 3], 2: [2, 3, 4], 3: [3, 4, 5], 4: [3, 4, 5, 5] }[Number(id)] || [];
}

function showError(err) {
  alert(err.message || String(err));
}

$("newGame").onclick = () => newGame().catch(showError);
$("refresh").onclick = () => refresh().catch(showError);
$("aiOne").onclick = () => aiStep().catch(showError);
$("aiRound").onclick = () => aiAdvanceRound().catch(showError);
$("autoAI").onchange = () => maybeAutoAI().catch(showError);
$("toggleShares").onclick = () => {
  revealAllShares = !revealAllShares;
  localStorage.setItem("manilaRevealAllShares", String(revealAllShares));
  if (state) renderPlayers();
};

refresh().catch(() => renderEmpty());