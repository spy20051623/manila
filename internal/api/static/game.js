let gameId = new URLSearchParams(window.location.search).get("game") || "";
let roomId = new URLSearchParams(window.location.search).get("room") || "";
let room = null;
let roomToken = new URLSearchParams(window.location.search).get("token") || localStorage.getItem("manilaRoomToken") || "";
if (roomToken) localStorage.setItem("manilaRoomToken", roomToken);
let gameSocket = null;
let reconnectTimer = null;
let state = null;
let actions = [];
let aiBusy = false;
let selectedGoods = new Set();
let shipStarts = {};
let navigatorMoves = {};
let toastTimer = null;
let localPlayerId = 0;
let settlementOverlayClosed = false;
let actionEventSeq = null;
let actionInputLocked = false;
let pendingSubmittedAction = null;
let animationsEnabled = localStorage.getItem("manilaAnimationsEnabled") !== "false";
let animationSnapshot = null;
let animationGameId = "";
let currentEventRecords = [];
let shipEventSnapshotTimeline = [];
let deferredRenderRoom = null;
let deferredRenderAllowed = false;
let deferredRenderApplying = false;
let playedAnimationGameId = "";
let forceAnimationBaseRender = false;
const playedAnimationEvents = new Set();
const activeAnimations = new Map();
const animationQueues = new Map();
const wsPayloadQueue = [];
let wsPayloadProcessing = false;
let initialRoomLoaded = false;
const DEBUG_SHOW_ALL_HOTSPOTS = false;
const DESIGN_VIEWPORT = { width: 1432, height: 828 };
const BOARD_ANIMATION_DURATION_MS = 1500;
const HELP_CONTENT = {
  global: {
    title: "全局",
    body: [
      "玩家扮演马尼拉港口的商人，通过竞拍港务长、购买货物股份、派遣同伙押注货船、港口、船坞、海盗船、领航员和保险商等位置来赚取比索。",
      "游戏目标是让自己的最终财富最高。最终财富 = 现金 + 持有股份的当前黑市价值 - 未赎回贷款惩罚。股份、贷款和破产细则见“玩家”帮助。",
      "游戏中有 4 种货物：人参、肉豆蔻、丝绸、玉石。每种货物都有黑市价值轨道：0、5、10、20、30。成功抵达马尼拉的货物会在回合结算后升值一格。",
      "任意一种货物的黑市价值达到 30 时，游戏会在当前回合结算完成后结束。最终财富最高的玩家获胜，财富相同可以并列。",
      "每轮称为一次航行。流程依次为：竞拍港务长、港务长购买股份、港务长选择 3 种出航货物、港务长设置 3 艘货船起点，然后进入放置同伙与移动货船的循环，最后结算。",
      "4 人规则下，每轮有 3 个放置阶段和 3 次移动阶段：放置同伙 1、移动 1、放置同伙 2、移动 2、放置同伙 3、领航员、移动 3、结算。",
      "港务长由每轮竞拍决定。竞拍中玩家可以出一个高于当前最高价的价格，或放弃竞拍；玩家一旦放弃，本轮竞拍不能再次出价。",
      "玩家不能出超过自己可支付能力的价格。可支付能力包括当前现金，以及可以通过抵押未抵押股份获得的贷款。最高出价者成为本轮港务长，并立即向港口钱箱支付出价。",
      "港务长在每轮开始时可以购买 1 张公开供应中的货物股份，也可以不买；随后从 4 种货物中选择 3 种装船出航，并设置这 3 艘货船的起点。",
      "货船起点必须是 0 到 5，且 3 艘出航货船起点数字总和必须等于 9。关于货船、到港、进入船坞和被海盗船掠夺的细则见“货船”帮助。",
      "放置阶段从港务长开始，按固定玩家顺序轮流行动。玩家必须放置 1 个同伙，不能主动放弃。各位置的规则分别见“货船”“港口”“船坞”“海盗船”“领航员”“保险商”帮助。",
      "移动阶段会掷出本轮出航货物对应的骰子，并分别移动对应货船。货船越过 13 立即到港；刚好停在 13 时需要根据海盗船规则处理；第 3 次移动后仍未到港的船进入船坞。",
      "回合结算内容包括：海盗船掠夺、确定货船进入港口或船坞、支付海盗收益、支付成功到港货船收益、支付港口和船坞收益、保险商赔付、货物升值，并回收同伙。",
      "全局区显示当前局号、回合、阶段、行动玩家和港务长。它用于判断当前处于哪一步流程，以及现在轮到哪位玩家行动。",
    ],
  },
  players: {
    title: "玩家",
    body: [
      "玩家区显示每名玩家的现金、股份、同伙状态和当前行动状态。每名玩家开局拥有 30 比索现金、随机 2 张初始货物股份和 3 个同伙。",
      "现金用于支付竞拍港务长的出价、购买货物股份、放置同伙的费用，以及保险员需要承担的船坞赔付。港务长出价会立即支付给港口钱箱。",
      "股份代表玩家持有的货物份额。每种货物有 5 张股份，货物黑市价值轨道为 0、5、10、20、30。港务长每轮最多购买 1 张公开供应中的股份，购买价格为该货物当前黑市价值，但最低价格为 5 比索。",
      "游戏结束时，玩家最终财富 = 现金 + 持有股份的当前黑市价值 - 未赎回贷款惩罚。每张未赎回贷款对应的抵押股份在终局扣 15 比索。",
      "玩家不需要主动抵押或赎回股份。只有当玩家必须支付费用但现金不足时，才会自动抵押未抵押股份获得贷款。每抵押 1 张股份，立即获得 12 比索；抵押股份仍属于玩家，但终局每张未赎回抵押股份扣 15 比索。",
      "可能触发自动抵押的强制支付包括：支付港务长竞拍价、支付放置同伙费用、保险员支付船坞赔偿。",
      "同伙是玩家每轮放置到货船、港口、船坞、海盗船、领航员岛或保险商位置的棋子。玩家放置同伙时必须有尚未使用的同伙、选择可用空位，并支付该位置要求的费用；保险商位置不需要支付费用。",
      "4 人规则下每轮有 3 个放置阶段，正常情况下每名玩家会在本轮放完自己的 3 个同伙。同一玩家可以在同一艘船、同一区域或多个区域放置多个同伙，只要位置合法。",
      "放置阶段不允许主动放弃。轮到玩家放置时，必须选择一个合法位置放置 1 个同伙。",
      "破产状态会在玩家放置前自动判定：当玩家没有可抵押股份，且当前现金低于当前局面下任何一个可行放置操作的费用时，会进入破产状态。进入破产状态后，本轮航行内无法解除。",
      "破产状态下，玩家不能选择普通放置位置，只能作为偷渡客放置到货船。若仍有现金，必须支付所有剩余现金；若现金为 0，则免费放置。",
      "破产玩家作为偷渡客时，只能选择当前可用费用最低的货船；如果多艘货船费用同为最低，只能选择 ID 最小的船。如果没有任何可用货船空位，会自动偷渡到 ID 最小的仍在本轮出航货船上。",
      "无空位偷渡不占用货船位置，也不影响该船后续普通登船格。无空位偷渡者不参与货船收益平分；该船正常船员先平分收益后，偷渡者获得等同于单个普通船员的收益。",
    ],
  },
  ships: {
    title: "货船",
    body: [
      "每轮只有 3 种货物会装船出航，剩余 1 种货物本轮不上船。未出航货物不能放置货船同伙，不会掷对应骰子，也不会在本轮升值。",
      "玩家可以把同伙放到仍在航行中、尚未到港的出航货船上。放置时必须使用该货船当前费用最低的空位，并立即支付对应费用。",
      "货船收益按货物固定：人参到港总收益 18，肉豆蔻 24，丝绸 30，玉石 36。对应登船费用分别为：人参 1/2/3，肉豆蔻 2/3/4，丝绸 3/4/5，玉石 3/4/5/5。",
      "如果货船成功抵达马尼拉，船上所有参与分红的同伙平分该货物总收益。",
      "如果货船未到港并进入船坞，船上同伙没有货船收益。如果货船被海盗船掠夺，船上原有同伙没有收益。",
      "每次移动阶段会掷出本轮出航货物对应的骰子，并分别移动对应货船。骰子点数必须完整移动。",
      "货船越过 13 时立即到港，多余点数忽略；货船刚好停在 13 时不会自动到港，需要按海盗船规则处理；第 3 次移动结束后仍未到港的船进入船坞。",
      "破产玩家可能作为偷渡客登上货船。偷渡客的支付、可选货船和收益规则见“玩家”帮助。",
    ],
  },
  actions: {
    title: "行动",
    body: [
      "行动区显示当前玩家在当前阶段可以执行的操作。每次轮到玩家行动时，必须从当前可执行行动中选择一个并完成。",
      "需要输入数值、选择货物、选择货船或确认目标时，相关控件会出现在行动区。",
      "常用放置操作也可以通过点击棋盘上对应区域的按钮触发，效果与在行动区选择相同。",
    ],
  },
  events: {
    title: "事件",
    body: [
      "事件区记录游戏发生过的关键事件，最新事件排在上方。",
      "这里用于回看竞拍、购买股份、选择货物、设置起点、放置同伙、货船移动、海盗船行动、收益结算和货物升值等结果。",
    ],
  },
  dock: {
    title: "船坞",
    body: [
      "船坞用于押注有货船未能抵达马尼拉。船坞共有 A、B、C 三个位置。",
      "放置费用和成功收益为：船坞 A 费用 4、收益 6；船坞 B 费用 3、收益 8；船坞 C 费用 2、收益 15。",
      "结算时，第 1 艘未到港的船进入船坞 A，第 2 艘未到港的船进入船坞 B，第 3 艘未到港的船进入船坞 C。",
      "如果对应船坞位置有船进入，该船坞上的同伙获得对应收益；如果对应船坞没有船，该船坞上的同伙没有收益。",
      "船坞收益优先由保险员支付；如果本轮没有保险员，则由港口钱箱支付。",
    ],
  },
  port: {
    title: "港口",
    body: [
      "港口用于押注有货船成功抵达马尼拉。港口共有 A、B、C 三个位置。",
      "放置费用和成功收益为：港口 A 费用 4、收益 6；港口 B 费用 3、收益 8；港口 C 费用 2、收益 15。",
      "结算时，第 1 艘到港的船对应港口 A，第 2 艘到港的船对应港口 B，第 3 艘到港的船对应港口 C。",
      "如果对应港口位置有船进入，该港口上的同伙获得对应收益；如果对应港口没有船，该港口上的同伙没有收益。",
      "成功到港的货物会在回合结算后升值一格。",
    ],
  },
  insurance: {
    title: "保险商",
    body: [
      "保险商位置不需要支付放置费用。玩家把同伙放到保险商位置时，立即从港口钱箱获得 10 比索。",
      "放在保险商位置的同伙成为本轮保险员。保险员承担本轮船坞赔付责任。",
      "结算时，如果有船进入船坞，且对应船坞位置有同伙获奖，则由保险员支付船坞收益。",
      "如果保险员本轮同时从其他位置获得收益，可以先收取收益，再支付赔偿。",
      "如果保险员现金不足，会自动抵押股份贷款支付。若保险员现金和贷款能力仍不足，则尽力支付，剩余部分由港口钱箱补足。",
      "如果本轮没有船坞收益需要支付，保险员无需赔偿，保留放置时获得的 10 比索。",
    ],
  },
  pirates: {
    title: "海盗船",
    body: [
      "海盗船有 2 个位置，每个位置费用为 5。先放置的位置是海盗船长，后放置的位置是副手。只有仍留在海盗船上的同伙会参与后续海盗登船、掠夺和收益分配。",
      "第 2 次掷骰移动结束后，如果有货船刚好停在 13，海盗可以尝试登船。海盗船长先决定是否登船，副手随后决定是否登船。",
      "海盗只能登上刚好停在 13 的货船，并且必须占用该货船的空格。如果目标货船没有空格，则不能登船。",
      "海盗一旦登船，会加入该货船并按货船乘员参与货船收益结算。已登船的海盗不再参与后续海盗决策或海盗收益。",
      "如果海盗船长登船且副手仍留在海盗船上，副手会被视作海盗船长。如果副手也登船，则海盗船上不再有海盗。",
      "第 3 次掷骰移动结束后，如果有货船刚好停在 13，且海盗船上仍有海盗，则海盗掠夺该船。被掠夺船上的原有同伙全部没有收益。",
      "海盗收益等于被掠夺货船的到港总收益，只分给第 3 次移动结束时仍留在海盗船上的海盗。已登船的海盗不参与分配，没有任何收益。",
      "掠夺后，由当前海盗船长决定该船进入港口还是船坞。进入港口时，该货物视为成功到港，回合结束后升值；进入船坞时，该货物不升值。",
      "如果第 3 次移动后船刚好停在 13，但海盗船上没有海盗，则该船视为成功到港。",
    ],
  },
  navigator: {
    title: "领航员",
    body: [
      "领航员位置在第 3 次掷骰移动前发动。领航员共有 2 个位置：小领航员和大领航员。",
      "小领航员放置费用为 2，可以将 1 艘未到港船前进或后退 1 格。",
      "大领航员放置费用为 5，可以将 1 艘未到港船移动最多 2 格，或将 2 艘未到港船各移动 1 格。",
      "领航员行动顺序为：小领航员先行动，大领航员后行动。",
      "领航员可以选择不移动。领航员可以向前或向后移动船。已经到港的船不能被领航员移动。",
      "领航员把船推过 13 时，该船立即到港。领航员把船推到 13 不触发海盗船；海盗船只在掷骰移动结束后触发。",
    ],
  },
};

function syncViewportScale() {
  const viewport = window.visualViewport;
  const viewportWidth = viewport?.width || window.innerWidth;
  const viewportHeight = viewport?.height || window.innerHeight;
  const offsetLeft = viewport?.offsetLeft || 0;
  const offsetTop = viewport?.offsetTop || 0;
  const safePad = 0;
  const availableWidth = Math.max(1, viewportWidth - safePad * 2);
  const availableHeight = Math.max(1, viewportHeight - safePad * 2);
  const scale = Math.min(availableWidth / DESIGN_VIEWPORT.width, availableHeight / DESIGN_VIEWPORT.height);
  const left = offsetLeft + Math.max(safePad, (viewportWidth - DESIGN_VIEWPORT.width * scale) / 2);
  const top = offsetTop + Math.max(safePad, (viewportHeight - DESIGN_VIEWPORT.height * scale) / 2);
  document.documentElement.style.setProperty("--ui-scale", String(scale));
  document.documentElement.style.setProperty("--ui-left", `${left}px`);
  document.documentElement.style.setProperty("--ui-top", `${top}px`);
}

syncViewportScale();
window.addEventListener("resize", syncViewportScale);
window.visualViewport?.addEventListener("resize", syncViewportScale);
window.visualViewport?.addEventListener("scroll", syncViewportScale);

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
  const headers = { "Content-Type": "application/json", ...(options.headers || {}) };
  if (roomToken) headers.Authorization = `Bearer ${roomToken}`;
  const res = await fetch(path, {
    headers,
    ...options,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.message || res.statusText);
  return data;
}

function getLocalPlayerId() {
  return Number(localPlayerId || 0);
}

function syncTokenToURL() {
  const url = new URL(window.location.href);
  if (roomToken) url.searchParams.set("token", roomToken);
  else url.searchParams.delete("token");
  if (roomId) url.searchParams.set("room", roomId);
  else url.searchParams.delete("room");
  window.history.replaceState({}, "", url);
}

function clearRoomToken() {
  if (!roomToken) return;
  roomToken = "";
  localStorage.removeItem("manilaRoomToken");
  syncTokenToURL();
  connectGameSocket();
}

function returnToLobby() {
  clearActiveAnimations(false);
  if (gameSocket) {
    gameSocket.onclose = null;
    gameSocket.close();
  }
  const url = new URL("/", window.location.origin);
  if (roomToken) url.searchParams.set("token", roomToken);
  if (roomId) url.searchParams.set("room", roomId);
  window.location.href = `${url.pathname}${url.search}`;
}

async function refresh(options = {}) {
  if (!gameId) {
    returnToLobby();
    return;
  }
  let data;
  try {
    data = await api(`/games/${encodeURIComponent(gameId)}/state`);
  } catch {
    returnToLobby();
    return;
  }
  clearActiveAnimations(false);
  wsPayloadQueue.length = 0;
  localPlayerId = Number(data.playerId || 0);
  await applyRoomPayload(roomFromGamePayload(data), { animate: false });
  initialRoomLoaded = true;
  if (state?.status === "ended") {
    lockFinishedGame();
    return;
  }
  if (options.auto !== false) {
    setTimeout(() => loadLegalActions().then(renderPostActionControls).catch(showError), 120);
  }
}

function roomFromGamePayload(data) {
  const game = data?.game || null;
  if (data?.roomId) roomId = data.roomId;
  return {
    roomId,
    status: game ? "inProgress" : "waiting",
    gameId: game?.gameId || gameId,
    game,
    eventSeq: game?.eventSeq || data?.eventSeq || 0,
    participant: { joined: Boolean(roomToken), playerId: Number(data?.playerId || 0) },
    canAIForCurrent: canLocalAIForGame(game),
  };
}

function lockFinishedGame() {
  actions = [];
  actionInputLocked = true;
  pendingSubmittedAction = null;
  hideBoardActionTargets();
  if (gameSocket) {
    gameSocket.onclose = null;
    gameSocket.close();
  }
  setBusyButtons(false);
  renderPostActionControls();
}

async function applyRoomPayload(nextRoom, options = {}) {
  const animate = Boolean(options.animate);
  if (nextRoom?.roomId) roomId = nextRoom.roomId;
  const nextEvents = eventRecordsFromGame(nextRoom?.game || null);
  if (isDuplicateInGamePayload(nextRoom)) {
    if (animate) {
      if (eventsArePrefix(nextEvents, currentEventRecords)) {
        if (nextEvents.length === currentEventRecords.length) {
          rememberDeferredRenderRoom(nextRoom, false);
        }
      } else {
        clearDeferredRenderRoom();
        clearActiveAnimations(false);
        await applyRoomPayload(nextRoom, { animate: false });
        console.info("已重绘");
      }
      syncAnimationToggleButton();
      setBusyButtons(false);
      return;
    }
    room = nextRoom || null;
    state = room?.game || null;
    gameId = state?.gameId || room?.gameId || "";
    currentEventRecords = eventRecordsFromGame(state);
    if (room?.participant?.joined) {
      syncTokenToURL();
    } else {
      clearRoomToken();
    }
    await loadLegalActions();
    syncTransientControls();
    renderRoom();
    animationSnapshot = captureAnimationSnapshot();
    rebuildShipEventSnapshotTimeline(currentEventRecords, animationSnapshot);
    syncAnimationToggleButton();
    setBusyButtons(false);
    return;
  }
  const previousSnapshot = animationSnapshot;
  const previousEvents = currentEventRecords;
  const nextSnapshot = captureAnimationSnapshotFromRoom(nextRoom);
  const sameAnimationGame = previousSnapshot
    && nextSnapshot
    && previousSnapshot.gameId === nextSnapshot.gameId;
  const incomingAlreadySeen = animate
    && sameAnimationGame
    && eventsArePrefix(nextEvents, previousEvents);
  if (incomingAlreadySeen) {
    if (nextEvents.length === previousEvents.length) {
      rememberDeferredRenderRoom(nextRoom, false);
    }
    syncAnimationToggleButton();
    setBusyButtons(false);
    return;
  }
  const eventRecordsMismatch = animate
    && sameAnimationGame
    && !eventsArePrefix(previousEvents, nextEvents);
  const canAnimateIncrementally = animate
    && sameAnimationGame
    && eventsArePrefix(previousEvents, nextEvents);
  if (canAnimateIncrementally) {
    if (leftRoundReviewTransition(previousSnapshot, nextSnapshot)) {
      clearDeferredRenderRoom();
      clearActiveAnimations(false);
      await applyRoomPayload(nextRoom, { animate: false });
      return;
    }
    rememberDeferredRenderRoom(
      nextRoom,
      shouldRedrawAfterIncrementalUpdate(previousSnapshot, nextSnapshot, previousEvents, nextEvents),
    );
    animationSnapshot = await enqueueNewEventAnimations(previousEvents, nextEvents, previousSnapshot, nextSnapshot) || nextSnapshot;
    currentEventRecords = nextEvents;
    if (nextSnapshot.game?.status === "ended") {
      actions = [];
      actionInputLocked = true;
      hideBoardActionTargets();
    }
    setBusyButtons(false);
    if (deferredRenderAllowed && !hasActiveOrQueuedAnimations()) scheduleDeferredRenderFlush();
    return;
  }
  const canBootstrapAnimations = animate
    && !previousSnapshot
    && nextSnapshot
    && nextRoom?.status === "inProgress"
    && nextEvents.length > 0;
  if (canBootstrapAnimations) {
    rememberDeferredRenderRoom(nextRoom, false);
    const initialSnapshot = createInitialAnimationSnapshot(nextSnapshot);
    animationSnapshot = await enqueueNewEventAnimations([], nextEvents, initialSnapshot, nextSnapshot) || nextSnapshot;
    currentEventRecords = nextEvents;
    setBusyButtons(false);
    return;
  }
  if (animate) {
    clearDeferredRenderRoom();
    clearActiveAnimations(false);
    if (eventRecordsMismatch) console.info("已重绘");
  }
  room = nextRoom || null;
  state = room?.game || null;
  gameId = state?.gameId || room?.gameId || "";
  if (room?.participant?.joined) {
    syncTokenToURL();
  } else {
    clearRoomToken();
  }
  resetTransientControls();
  if (animate) {
    actions = [];
    actionEventSeq = state?.eventSeq ?? null;
  } else {
    await loadLegalActions();
  }
  syncTransientControls();
  renderRoom();
  animationSnapshot = captureAnimationSnapshot();
  currentEventRecords = nextEvents;
  rebuildShipEventSnapshotTimeline(currentEventRecords, animationSnapshot);
  if (state?.status === "ended") {
    lockFinishedGame();
    return;
  }
  if (animate) {
    loadLegalActions().then(renderPostActionControls).catch(showError);
  }
}

function isDuplicateInGamePayload(nextRoom) {
  const nextState = nextRoom?.game || null;
  if (!room || !state || !nextRoom || !nextState) return false;
  if (room.status !== "inProgress" || nextRoom.status !== "inProgress") return false;
  if ((state.gameId || "") !== (nextState.gameId || "")) return false;
  return Number(state.eventSeq || 0) === Number(nextState.eventSeq || 0);
}

function enqueueWSPayload(nextRoom) {
  if (!initialRoomLoaded) return;
  wsPayloadQueue.push(nextRoom);
  if (!wsPayloadProcessing) processWSPayloadQueue().catch(showError);
}

async function processWSPayloadQueue() {
  wsPayloadProcessing = true;
  try {
    while (wsPayloadQueue.length) {
      const nextRoom = wsPayloadQueue.shift();
      await applyRoomPayload(nextRoom, { animate: true });
    }
  } finally {
    wsPayloadProcessing = false;
  }
}

function eventRecordsFromGame(game) {
  return Array.isArray(game?.events) ? game.events : [];
}

function eventsArePrefix(prefix, full) {
  if (!Array.isArray(prefix) || !Array.isArray(full)) return false;
  if (prefix.length > full.length) return false;
  for (let index = 0; index < prefix.length; index++) {
    if (!sameEventRecord(prefix[index], full[index])) return false;
  }
  return true;
}

function sameEventRecord(a, b) {
  return Number(a?.seq || 0) === Number(b?.seq || 0) && String(a?.type || "") === String(b?.type || "");
}

function leftRoundReviewTransition(previousSnapshot, nextSnapshot) {
  return previousSnapshot
    && nextSnapshot
    && previousSnapshot.gameId === nextSnapshot.gameId
    && previousSnapshot.phase === "RoundReview"
    && nextSnapshot.phase !== "RoundReview";
}

function rememberDeferredRenderRoom(nextRoom, allowRender) {
  if (!nextRoom) return;
  deferredRenderRoom = nextRoom;
  deferredRenderAllowed = Boolean(deferredRenderAllowed || allowRender);
}

function clearDeferredRenderRoom() {
  deferredRenderRoom = null;
  deferredRenderAllowed = false;
}

function shouldRedrawAfterIncrementalUpdate(previousSnapshot, nextSnapshot, previousEvents, nextEvents) {
  if (!previousSnapshot || !nextSnapshot) return false;
  if (nextSnapshot.game?.status === "ended") return true;
  if (pendingSubmittedAction && Number(nextSnapshot.eventSeq || 0) > Number(pendingSubmittedAction.expectedEventSeq || 0)) return true;
  if (!isLocalActionWindowForGame(previousSnapshot.game) && isLocalActionWindowForGame(nextSnapshot.game)) return true;
  return false;
}

async function enqueueNewEventAnimations(previousEvents, nextEvents, previousSnapshot, nextSnapshot) {
  syncAnimationToggleButton();
  if (!nextSnapshot) {
    clearActiveAnimations(true);
    animationGameId = "";
    return null;
  }
  if (nextSnapshot.gameId !== animationGameId) {
    if (animationGameId) {
      animationGameId = nextSnapshot.gameId;
      resetPlayedAnimationEvents(nextSnapshot.gameId);
      clearActiveAnimations(true);
    } else {
      animationGameId = nextSnapshot.gameId;
    }
  }
  if (nextSnapshot.gameId !== playedAnimationGameId) resetPlayedAnimationEvents(nextSnapshot.gameId);
  if (leftRoundReviewTransition(previousSnapshot, nextSnapshot)) clearActiveAnimations(true);
  if (!previousSnapshot || previousSnapshot.gameId !== nextSnapshot.gameId) {
    return null;
  }
  const newEvents = nextEvents.slice(previousEvents.length);
  const eventSnapshot = cloneAnimationSnapshot(previousSnapshot);
  for (const event of newEvents) {
    const key = animationEventKey(event);
    const alreadyPlayed = playedAnimationEvents.has(key);
    if (!alreadyPlayed) playedAnimationEvents.add(key);
    const result = playEventAnimation(event, eventSnapshot, nextSnapshot, { animate: !alreadyPlayed });
    if (result?.waitForPaint) await waitForNextPaint();
    recordShipEventSnapshot(event, eventSnapshot);
  }
  eventSnapshot.phase = nextSnapshot.phase;
  eventSnapshot.roundNumber = nextSnapshot.roundNumber;
  eventSnapshot.eventSeq = nextSnapshot.eventSeq;
  eventSnapshot.events = nextSnapshot.events;
  eventSnapshot.game = nextSnapshot.game;
  return eventSnapshot;
}

function resetPlayedAnimationEvents(nextGameId) {
  playedAnimationGameId = nextGameId || "";
  playedAnimationEvents.clear();
  resetShipEventSnapshotTimeline();
}

function animationEventKey(event) {
  return `${Number(event?.seq || 0)}:${String(event?.type || "")}`;
}

function resetShipEventSnapshotTimeline() {
  shipEventSnapshotTimeline = [];
}

function createInitialAnimationSnapshot(nextSnapshot) {
  const baseGame = clonePlain(nextSnapshot?.game || {});
  baseGame.ships = {};
  baseGame.round = {
    ...(baseGame.round || {}),
    selectedGoods: [],
    excludedGoods: undefined,
  };
  return {
    ...nextSnapshot,
    phase: "",
    eventSeq: 0,
    events: [],
    game: baseGame,
    ships: {},
  };
}

function renderAnimationBaseFromGame(sourceGame) {
  if (!sourceGame) return;
  const liveRoom = room;
  const liveState = state;
  const liveGameId = gameId;
  const liveActions = actions;
  const liveActionEventSeq = actionEventSeq;
  const liveForceRender = forceAnimationBaseRender;
  try {
    forceAnimationBaseRender = true;
    room = { ...(deferredRenderRoom || room || {}), status: "inProgress", game: sourceGame };
    state = sourceGame;
    gameId = sourceGame.gameId || room?.gameId || "";
    actions = [];
    actionEventSeq = sourceGame.eventSeq ?? null;
    syncTransientControls();
    renderRoom();
  } finally {
    forceAnimationBaseRender = liveForceRender;
    room = liveRoom;
    state = liveState;
    gameId = liveGameId;
    actions = liveActions;
    actionEventSeq = liveActionEventSeq;
    syncTransientControls();
  }
}

function enqueueAnimationBaseRender(sourceGame) {
  if (!sourceGame) return;
  queueAnimation("ship:group", (done) => {
    renderAnimationBaseFromGame(sourceGame);
    waitForNextPaint().then(done).catch(() => done());
    return null;
  });
}

function waitForNextPaint() {
  return new Promise((resolve) => {
    if (typeof requestAnimationFrame !== "function") {
      setTimeout(resolve, 0);
      return;
    }
    requestAnimationFrame(() => requestAnimationFrame(resolve));
  });
}

function buildGoodsSelectedBaseGame(data, sourceGame, event) {
  const base = clonePlain(sourceGame);
  if (!base) return null;
  const goodsIds = (data?.goodsIds || base.round?.selectedGoods || []).map(Number).filter(Boolean);
  const ships = {};
  goodsIds.forEach((shipId, index) => {
    ships[String(shipId)] = {
      shipId,
      goodsId: shipId,
      routeIndex: index + 1,
      position: 0,
      status: "sailing",
      occupants: [],
      stowaways: [],
      lootedByPirates: false,
      nextBoardingSlot: 0,
    };
  });
  base.phase = "HarborMasterSetShips";
  base.currentPlayer = base.harborMaster || base.currentPlayer || 0;
  base.ships = ships;
  base.round = {
    ...(base.round || {}),
    selectedGoods: goodsIds,
    excludedGoods: data?.excludedGoodsId,
    placementStep: 0,
    movementStep: 0,
    placementTurnsTaken: 0,
    diceResults: [],
    pendingPirateQueue: [],
    pendingLootShips: [],
    navigatorStep: "",
    arrivedOrder: [],
    dockedOrder: [],
    confirmedPlayers: {},
  };
  base.board = emptyBoardForAnimation(base.board || {});
  base.eventSeq = Number(event?.seq || base.eventSeq || 0);
  base.events = Array.isArray(base.events)
    ? base.events.filter((item) => Number(item?.seq || 0) <= base.eventSeq)
    : [];
  return base;
}

function emptyBoardForAnimation(board) {
  const cleanSlots = (slots) => {
    const out = {};
    for (const [id, slot] of Object.entries(slots || {})) {
      out[id] = { ...slot };
      delete out[id].occupant;
    }
    return out;
  };
  return {
    ...board,
    ports: cleanSlots(board.ports),
    docks: cleanSlots(board.docks),
    pirates: [],
    boardedPirates: [],
    smallNavigator: null,
    bigNavigator: null,
    insurance: null,
  };
}

function updateAnimationSnapshotFromGame(targetSnapshot, sourceGame) {
  if (!targetSnapshot || !sourceGame) return;
  const next = captureAnimationSnapshotFromRoom({ status: "inProgress", game: sourceGame });
  if (!next) return;
  Object.assign(targetSnapshot, next);
}

function clonePlain(value) {
  if (value === null || value === undefined) return value;
  return JSON.parse(JSON.stringify(value));
}

function cloneAnimationSnapshot(snapshot) {
  if (!snapshot) return null;
  const ships = {};
  for (const [shipId, ship] of Object.entries(snapshot.ships || {})) {
    ships[shipId] = {
      ...ship,
      point: ship.point ? { ...ship.point } : null,
    };
  }
  return { ...snapshot, ships, game: snapshot.game || null };
}

function cloneShipFrame(ships) {
  const frame = {};
  for (const [shipId, ship] of Object.entries(ships || {})) {
    frame[shipId] = {
      position: Number(ship?.position ?? -1),
      status: ship?.status || "",
      routeIndex: Number(ship?.routeIndex || 1),
    };
  }
  return frame;
}

function recordShipEventSnapshot(event, snapshot) {
  if (!event || !snapshot) return;
  shipEventSnapshotTimeline.push({
    seq: Number(event.seq || 0),
    type: String(event.type || ""),
    ships: shipPositionsOnly(snapshot.ships || {}),
  });
  if (shipEventSnapshotTimeline.length > 1000) {
    shipEventSnapshotTimeline.splice(0, shipEventSnapshotTimeline.length - 1000);
  }
}

function shipPositionsOnly(ships) {
  const positions = {};
  for (const [shipId, ship] of Object.entries(ships || {})) {
    positions[shipId] = Number(ship?.position ?? -1);
  }
  return positions;
}

function rebuildShipEventSnapshotTimeline(events, finalSnapshot) {
  resetShipEventSnapshotTimeline();
  if (!finalSnapshot || !Array.isArray(events) || events.length === 0) return;
  const snapshot = createInitialAnimationSnapshot(finalSnapshot);
  for (const event of events) {
    applyEventToShipSnapshot(event, snapshot, finalSnapshot);
    recordShipEventSnapshot(event, snapshot);
  }
}

function applyEventToShipSnapshot(event, snapshot, nextSnapshot) {
  if (!event || !snapshot) return;
  if (event.type === "GoodsSelected") {
    const baseGame = buildGoodsSelectedBaseGame(event.data || {}, nextSnapshot?.game || snapshot.game || null, event);
    updateAnimationSnapshotFromGame(snapshot, baseGame);
    return;
  }
  if (event.type === "ShipStartsSet") {
    applyShipStartsToSnapshot(event.data?.starts || {}, snapshot, nextSnapshot);
    return;
  }
  if (event.type === "DiceRolled") {
    applyDiceToSnapshot(event.data?.results || {}, snapshot);
    return;
  }
  if (event.type === "NavigatorMoved") {
    applyNavigatorToSnapshot(event.data?.moves || [], snapshot);
    return;
  }
  if (event.type === "RoundStarted") {
    clearShipPositionSnapshot(snapshot);
    return;
  }
  if (event.type === "ShipArrived" || event.type === "ShipDocked") {
    applyShipStatusToSnapshot(event.data?.shipId, event.type, snapshot, { queue: false });
  }
}

function captureAnimationSnapshot() {
  return captureAnimationSnapshotFromRoom(room);
}

function captureAnimationSnapshotFromRoom(sourceRoom) {
  const sourceState = sourceRoom?.game || null;
  if (!sourceRoom || !sourceState || sourceRoom.status !== "inProgress") return null;
  const selected = selectedShipIdsForGame(sourceState);
  const ships = {};
  selected.forEach((shipId, index) => {
    const ship = getShipFromGame(sourceState, shipId);
    if (!ship) return;
    const routeIndex = Number(ship.routeIndex || index + 1);
    const boardPosition = shipBoardPositionForAnimation(ship.position, ship.status);
    ships[shipId] = {
      position: Number(ship.position || 0),
      status: ship.status || "",
      routeIndex,
      point: shipPoint(boardPosition, routeIndex),
    };
  });
  return {
    gameId: sourceState.gameId || "",
    phase: sourceState.phase || "",
    roundNumber: Number(sourceState.roundNumber || 0),
    eventSeq: Number(sourceState.eventSeq || 0),
    events: Array.isArray(sourceState.events) ? sourceState.events : [],
    game: sourceState,
    ships,
  };
}

function applyShipStartsToSnapshot(starts, snapshot, nextSnapshot) {
  if (!snapshot) return;
  const selected = selectedShipIdsForGame(snapshot.game || nextSnapshot?.game || null);
  for (const shipId of selected) {
    const current = snapshot.ships?.[shipId] || snapshot.ships?.[String(shipId)] || shipSnapshotFromFinal(shipId, nextSnapshot);
    if (!current) continue;
    const start = Number(starts?.[shipId] ?? starts?.[String(shipId)] ?? current.position ?? 0);
    snapshot.ships[shipId] = shipSnapshotWithPosition(current, start);
  }
}

function applyDiceToSnapshot(results, snapshot) {
  if (!snapshot) return;
  for (const [shipId, roll] of Object.entries(results || {})) {
    applyShipDeltaToSnapshot(shipId, Number(roll || 0), snapshot);
  }
}

function applyNavigatorToSnapshot(moves, snapshot) {
  if (!snapshot) return;
  for (const move of moves || []) {
    applyShipDeltaToSnapshot(move?.shipId, Number(move?.delta || 0), snapshot);
  }
}

function applyShipDeltaToSnapshot(shipId, delta, snapshot) {
  const id = Number(shipId);
  const ship = snapshot?.ships?.[id] || snapshot?.ships?.[String(id)];
  if (!ship || Number(ship.position) < 0) return;
  snapshot.ships[id] = shipSnapshotWithPosition(ship, Math.max(0, Number(ship.position || 0) + Number(delta || 0)));
}

function shipSnapshotFromFinal(shipId, nextSnapshot) {
  const id = Number(shipId);
  const ship = nextSnapshot?.ships?.[id] || nextSnapshot?.ships?.[String(id)];
  return ship ? { ...ship, point: ship.point ? { ...ship.point } : null } : null;
}

function shipSnapshotWithPosition(ship, position) {
  const next = {
    ...ship,
    position: Number(position || 0),
  };
  next.point = shipPointForSnapshotShip(next);
  return next;
}

function playEventAnimation(event, previousSnapshot, nextSnapshot, options = {}) {
  if (!event || !event.type) return;
  const animate = options.animate !== false;
  const beforeShips = cloneShipFrame(previousSnapshot?.ships || {});
  if (event.type === "GoodsSelected") {
    const baseGame = buildGoodsSelectedBaseGame(event.data || {}, nextSnapshot?.game || null, event);
    if (!baseGame) return;
    if (animate) enqueueAnimationBaseRender(baseGame);
    updateAnimationSnapshotFromGame(previousSnapshot, baseGame);
    return;
  }
  if (event.type === "ShipStartsSet") {
    applyShipStartsToSnapshot(event.data?.starts || {}, previousSnapshot, nextSnapshot);
    if (animate) playShipStartsAnimation(event.data?.starts || {}, beforeShips, previousSnapshot?.ships || {});
    return;
  }
  if (event.type === "RoundStarted") {
    clearShipPositionSnapshot(previousSnapshot);
    return;
  }
  if (event.type === "AccomplicePlaced") {
    queuePlacedPieceUpdate(event.data || {}, nextSnapshot?.game || null);
    return;
  }
  if (event.type === "PirateBoarded") {
    queuePirateBoardedPieceUpdate(event.data || {}, nextSnapshot?.game || null);
    showPirateCue(event.data?.shipId);
    return;
  }
  if (event.type === "DiceRolled") {
    applyDiceToSnapshot(event.data?.results || {}, previousSnapshot);
    if (animate) playDiceAnimation(event.data?.results || {}, beforeShips, previousSnapshot?.ships || {});
    return;
  }
  if (event.type === "NavigatorMoved") {
    applyNavigatorToSnapshot(event.data?.moves || [], previousSnapshot);
    if (animate) playNavigatorAnimation(event.data?.moves || [], beforeShips, previousSnapshot?.ships || {});
    return;
  }
  if (event.type === "PiratesLootedShip") {
    showPirateCue(event.data?.shipId);
    return;
  }
  if (event.type === "ShipArrived" || event.type === "ShipDocked") {
    applyShipStatusToSnapshot(event.data?.shipId, event.type, previousSnapshot);
  }
}

function playDiceAnimation(results, beforeShips, afterShips) {
  const animations = [];
  for (const [shipId, roll] of Object.entries(results || {})) {
    animations.push(buildShipFrameAnimation(shipId, beforeShips, afterShips, `+${roll}`));
  }
  enqueueShipAnimationGroup(animations);
}

function playNavigatorAnimation(moves, beforeShips, afterShips) {
  const animations = [];
  for (const move of moves || []) {
    const delta = Number(move?.delta || 0);
    if (!delta) continue;
    animations.push(buildShipFrameAnimation(move.shipId, beforeShips, afterShips, `${delta > 0 ? "+" : ""}${delta}`));
  }
  enqueueShipAnimationGroup(animations);
}

function playShipStartsAnimation(starts, beforeShips, afterShips) {
  const selected = Object.keys(afterShips || {}).map(Number).sort((a, b) => a - b);
  const animations = [];
  for (const shipId of selected) {
    const start = Number(starts?.[shipId] ?? starts?.[String(shipId)] ?? 0);
    animations.push(buildShipFrameAnimation(shipId, beforeShips, afterShips, `\u8d77\u70b9 ${start}`));
  }
  enqueueShipAnimationGroup(animations);
}

function buildShipFrameAnimation(shipId, beforeShips, afterShips, label) {
  const id = Number(shipId);
  const before = beforeShips?.[id] || beforeShips?.[String(id)];
  const after = afterShips?.[id] || afterShips?.[String(id)];
  if (!before || !after || Number(before.position) < 0 || Number(after.position) < 0) return null;
  const startPoint = shipPointForSnapshotShip(before);
  const endPoint = shipPointForSnapshotShip(after);
  const moved = pointDistance(startPoint, endPoint) > 0.1;
  if (!moved && !label) return null;
  return {
    shipId: id,
    startPoint,
    endPoint,
    label,
    moved,
    beforeShipInfo: { ...before, point: startPoint },
    shipInfo: { ...after, point: endPoint },
  };
}

function shipPointForSnapshotShip(ship) {
  if (!ship || Number(ship.position) < 0) return null;
  return shipPoint(shipBoardPositionForAnimation(ship.position, ship.status), ship.routeIndex || 1);
}

function clearShipPositionSnapshot(snapshot) {
  if (!snapshot) return;
  for (const ship of Object.values(snapshot.ships || {})) {
    ship.position = -1;
    ship.point = null;
  }
}

function applyShipStatusToSnapshot(shipId, eventType, snapshot, options = {}) {
  const id = Number(shipId);
  const ship = snapshot?.ships?.[id] || snapshot?.ships?.[String(id)];
  if (!ship || !snapshot?.ships) return;
  const status = eventType === "ShipArrived" ? "arrived" : "docked";
  const updated = {
    ...ship,
    status,
  };
  updated.point = shipPointForSnapshotShip(updated);
  snapshot.ships[id] = updated;
  if (options.queue !== false) enqueueShipInfoUpdate(id, updated);
}

function syncEventShipToFinalSnapshot(shipId, eventSnapshot, nextSnapshot, options = {}) {
  const id = Number(shipId);
  const finalShip = nextSnapshot?.ships?.[id];
  if (!finalShip || !eventSnapshot?.ships) return;
  eventSnapshot.ships[id] = {
    ...finalShip,
    point: finalShip.point ? { ...finalShip.point } : null,
  };
  if (options.queue !== false) enqueueShipInfoUpdate(id, eventSnapshot.ships[id]);
}

function pointDistance(a, b) {
  if (!a || !b) return 0;
  return Math.abs(Number(a.x) - Number(b.x)) + Math.abs(Number(a.y) - Number(b.y));
}

function queuePlacedPieceUpdate(data, sourceGame) {
  const key = placementVisualQueueKey(data);
  const commit = () => syncPlacementCell(data, sourceGame, pieceFromEventData(data));
  queueAnimation(key, (done) => {
    commit();
    const target = placedPieceElement(data);
    if (!animationsEnabled || !target) {
      done();
      return null;
    }
    return startElementPulse(target, "anim-piece-pulse", done);
  }, commit);
}

function placementVisualQueueKey(data) {
  const positionType = data.positionType || "";
  if (positionType === "ship") {
    if (data.stowaway) return `cell:stowaway:${data.shipId}:${data.stowawaySlot ?? data.playerId ?? ""}`;
    return `cell:ship:${data.shipId}:${shipSeatIndexFromEventData(data)}`;
  }
  if (positionType === "port" || positionType === "dock") return `cell:${positionType}:${data.slot}`;
  if (positionType === "pirate") return `cell:pirate:${slotIdFromEventData(data, specialPieceSlotIndex("pirate", data.playerId))}`;
  if (positionType === "navigatorSmall") return "cell:navigatorSmall:small";
  if (positionType === "navigatorBig") return "cell:navigatorBig:big";
  if (positionType === "insurance") return "cell:insurance:insurance";
  return `cell:${positionType || "unknown"}:${data.playerId || ""}`;
}

function syncPlacementCell(data, sourceGame, fallbackPiece = undefined) {
  const positionType = data.positionType || "";
  if (positionType === "ship") {
    if (!data.stowaway) syncShipSeatCell(data.shipId, data.playerId, sourceGame, shipSeatIndexFromEventData(data), fallbackPiece);
    return;
  }
  if (positionType === "port") {
    syncBoardSlotCell("port", data.slot, sourceGame, fallbackPiece);
    return;
  }
  if (positionType === "dock") {
    syncBoardSlotCell("dock", data.slot, sourceGame, fallbackPiece);
    return;
  }
  if (positionType === "pirate") {
    syncSpecialSlotCell("pirate", slotIdFromEventData(data, specialPieceSlotIndex("pirate", data.playerId)), sourceGame, fallbackPiece);
    return;
  }
  if (positionType === "navigatorSmall") {
    syncSpecialSlotCell("navigatorSmall", "small", sourceGame, fallbackPiece);
    return;
  }
  if (positionType === "navigatorBig") {
    syncSpecialSlotCell("navigatorBig", "big", sourceGame, fallbackPiece);
    return;
  }
  if (positionType === "insurance") {
    syncSpecialSlotCell("insurance", "insurance", sourceGame, fallbackPiece);
  }
}

function placedPieceElement(data) {
  const positionType = data.positionType || "";
  if (positionType === "ship") {
    if (data.stowaway) return null;
    return shipSeatElement(data.shipId, shipSeatIndexFromEventData(data));
  }
  if (positionType === "port" || positionType === "dock") {
    return pieceInSlotElement(positionType, data.slot, data.playerId);
  }
  if (positionType === "pirate") {
    return pieceInSlotElement("pirate", slotIdFromEventData(data, specialPieceSlotIndex("pirate", data.playerId)), data.playerId);
  }
  if (positionType === "navigatorSmall" || positionType === "navigatorBig" || positionType === "insurance") {
    return pieceInSlotElement(positionType, slotIdFromEventData(data, positionType === "insurance" ? "insurance" : (positionType === "navigatorSmall" ? "small" : "big")), data.playerId);
  }
  return null;
}

function shipSeatElement(shipId, seatIndex) {
  return document.querySelector(`.top-ship-seat[data-ship-id="${selectorValue(shipId)}"][data-seat-index="${Number(seatIndex)}"]`);
}

function pieceInSlotElement(kind, slotId, playerId) {
  return document.querySelector(`.board-slot[data-kind="${selectorValue(kind)}"][data-slot-id="${selectorValue(slotId)}"] .piece[data-player-id="${selectorValue(playerId)}"]`);
}

function slotIdFromEventData(data, fallback = "") {
  if (data?.slot !== undefined && data?.slot !== null && data.slot !== "") return data.slot;
  return fallback;
}

function shipSeatIndexFromEventData(data) {
  if (data?.slot !== undefined && data?.slot !== null && !Number.isNaN(Number(data.slot))) return Number(data.slot);
  if (data?.boardingSlot !== undefined && data?.boardingSlot !== null && !Number.isNaN(Number(data.boardingSlot))) {
    return Math.max(0, Number(data.boardingSlot) - 1);
  }
  return shipSeatIndexForPlayer(data?.shipId, data?.playerId);
}

function shipSeatIndexForPlayer(shipId, playerId) {
  const ship = getShipFromGame(deferredRenderRoom?.game || null, Number(shipId))
    || getShipFromGame(state, Number(shipId));
  const occupants = ship?.occupants || [];
  for (let index = occupants.length - 1; index >= 0; index--) {
    if (Number(occupants[index]?.playerId) === Number(playerId)) return index;
  }
  return Math.max(0, occupants.length - 1);
}

function specialPieceSlotIndex(kind, playerId) {
  if (kind !== "pirate") return "";
  const pieces = deferredRenderRoom?.game?.board?.pirates || state?.board?.pirates || [];
  for (let index = pieces.length - 1; index >= 0; index--) {
    if (Number(pieces[index]?.playerId) === Number(playerId)) return index + 1;
  }
  return Math.max(1, pieces.length);
}

function syncShipSeatCell(shipId, playerId, sourceGame, seatIndex = null, fallbackPiece = null) {
  const ship = getShipFromGame(sourceGame, Number(shipId));
  if (!ship && (seatIndex === null || seatIndex === undefined)) return;
  const resolvedSeatIndex = seatIndex === null || seatIndex === undefined
    ? shipSeatIndexForPlayerFromShip(ship, playerId)
    : Number(seatIndex);
  const target = document.querySelector(`.top-ship-seat[data-ship-id="${selectorValue(shipId)}"][data-seat-index="${resolvedSeatIndex}"]`);
  const sourceOccupant = (ship?.occupants || [])[resolvedSeatIndex];
  const occupant = sourceOccupant && Number(sourceOccupant.playerId) === Number(playerId)
    ? sourceOccupant
    : (fallbackPiece || sourceOccupant);
  const cost = boardingCosts(Number(shipId))[resolvedSeatIndex] ?? "";
  updateTopShipSeatElement(target, Number(shipId), resolvedSeatIndex, occupant, cost);
}

function pieceFromEventData(data) {
  if (!data || data.playerId === undefined || data.playerId === null) return null;
  return {
    playerId: Number(data.playerId),
    role: data.shipRole || data.role || "",
  };
}

function shipSeatIndexForPlayerFromShip(ship, playerId) {
  const occupants = ship?.occupants || [];
  for (let index = occupants.length - 1; index >= 0; index--) {
    if (Number(occupants[index]?.playerId) === Number(playerId)) return index;
  }
  return Math.max(0, occupants.length - 1);
}

function updateTopShipSeatElement(target, shipId, seatIndex, occupant, cost) {
  if (!target) return;
  target.classList.toggle("occupied", Boolean(occupant));
  target.dataset.shipId = String(shipId);
  target.dataset.seatIndex = String(seatIndex);
  if (occupant) {
    target.dataset.playerId = String(Number(occupant.playerId));
    target.dataset.pieceRole = occupant.role || "";
    target.setAttribute("style", pieceStyle(occupant));
    target.textContent = `P${occupant.playerId}`;
  } else {
    delete target.dataset.playerId;
    delete target.dataset.pieceRole;
    target.removeAttribute("style");
    target.textContent = cost;
  }
}

function syncBoardSlotCell(kind, slotId, sourceGame, fallbackPiece = undefined) {
  const slot = kind === "port"
    ? sourceGame?.board?.ports?.[slotId]
    : sourceGame?.board?.docks?.[slotId];
  const target = document.querySelector(`.board-slot[data-kind="${selectorValue(kind)}"][data-slot-id="${selectorValue(slotId)}"]`);
  updateBoardSlotPieceElement(target, fallbackPiece === undefined ? slot?.occupant : fallbackPiece);
}

function syncSpecialSlotCell(kind, slotId, sourceGame, fallbackPiece = undefined) {
  let piece = null;
  if (kind === "pirate") piece = (sourceGame?.board?.pirates || [])[Math.max(0, Number(slotId) - 1)];
  if (kind === "navigatorSmall") piece = sourceGame?.board?.smallNavigator;
  if (kind === "navigatorBig") piece = sourceGame?.board?.bigNavigator;
  if (kind === "insurance") piece = sourceGame?.board?.insurance;
  const target = document.querySelector(`.board-slot[data-kind="${selectorValue(kind)}"][data-slot-id="${selectorValue(slotId)}"]`);
  updateBoardSlotPieceElement(target, fallbackPiece === undefined ? piece : fallbackPiece);
}

function updateBoardSlotPieceElement(target, piece) {
  if (!target) return;
  target.classList.toggle("filled", Boolean(piece));
  const name = target.querySelector(".slot-name span:last-child");
  if (name) name.textContent = piece ? pieceText(piece) : "空";
  const row = target.querySelector(".piece-row");
  if (row) row.innerHTML = piece ? pieceHTML(piece) : pieceHTML(null);
  const labels = target.querySelector(".slot-labels");
  if (labels) labels.hidden = Boolean(piece);
}

function enqueueShipAnimationGroup(items) {
  const animations = (items || []).filter(Boolean);
  if (!animations.length) return;
  queueAnimation("ship:group", (done) => {
    let remaining = animations.length;
    const cleanups = [];
    const finishOne = () => {
      remaining--;
      if (remaining <= 0) done();
    };
    for (const item of animations) {
      cleanups.push(startShipAnimation(item, finishOne));
    }
    return {
      finish: (snap) => {
        for (const cleanup of cleanups) cleanup?.finish?.(snap);
      },
    };
  }, () => commitShipAnimationGroup(animations));
}

function commitShipAnimationGroup(items) {
  for (const item of items || []) {
    if (!item) continue;
    const ship = shipElement(item.shipId);
    if (item.moved) finishShipMove(ship, item.endPoint);
    updateShipElementInfo(ship, item.shipInfo);
    syncStateShipInfo(item.shipId, item.shipInfo);
    redrawShipFromSnapshot(item.shipId, item.shipInfo);
    clearShipFloatBadges(ship);
  }
}

function startShipAnimation(item, done) {
  const { shipId, startPoint, endPoint, label, moved, shipInfo, beforeShipInfo } = item;
  if (beforeShipInfo) redrawShipFromSnapshot(shipId, beforeShipInfo);
  const ship = shipElement(shipId);
  if (!ship) {
    done();
    return null;
  }
  let frame = 0;
  let badge = null;
  let infoTimer = 0;
  if (moved) {
    if (!animationsEnabled) {
      finishShipMove(ship, endPoint);
      updateShipElementInfo(ship, shipInfo);
      done();
      return null;
    }
    ship.classList.remove("animating-move");
    ship.style.transition = "none";
    ship.style.left = `${startPoint.x}%`;
    ship.style.top = `${startPoint.y}%`;
    void ship.offsetWidth;
    ship.style.transition = "";
    ship.classList.add("animating-move");
    frame = requestAnimationFrame(() => {
      ship.style.left = `${endPoint.x}%`;
      ship.style.top = `${endPoint.y}%`;
    });
    infoTimer = setTimeout(() => {
      finishShipMove(ship, endPoint);
      updateShipElementInfo(ship, shipInfo);
    }, 680);
  } else if (shipInfo) {
    updateShipElementInfo(ship, shipInfo);
  }
  if (label) {
    clearShipFloatBadges(ship);
    if (animationsEnabled) {
      badge = document.createElement("span");
      badge.className = "ship-float";
      badge.textContent = label;
      ship.appendChild(badge);
    }
  }
  if (!animationsEnabled) {
    done();
    return null;
  }
  const timer = setTimeout(() => {
    if (moved) finishShipMove(ship, endPoint);
    updateShipElementInfo(ship, shipInfo);
    if (badge) badge.remove();
    done();
  }, BOARD_ANIMATION_DURATION_MS);
  return {
    finish: (snap) => {
      cancelAnimationFrame(frame);
      clearTimeout(infoTimer);
      clearTimeout(timer);
      if (snap && moved) finishShipMove(ship, endPoint);
      if (snap) {
        updateShipElementInfo(ship, shipInfo);
        syncStateShipInfo(shipId, shipInfo);
        redrawShipFromSnapshot(shipId, shipInfo);
      }
      if (badge) badge.remove();
    },
  };
}

function clearShipFloatBadges(ship) {
  ship?.querySelectorAll(".ship-float").forEach((item) => item.remove());
}

function enqueueShipInfoUpdate(shipId, shipInfo) {
  if (!shipInfo) return;
  const commit = () => {
    updateShipElementInfo(shipElement(shipId), shipInfo);
    syncStateShipInfo(shipId, shipInfo);
    redrawShipFromSnapshot(shipId, shipInfo);
  };
  queueAnimation("ship:group", (done) => {
    commit();
    done();
    return null;
  }, commit);
}

function updateShipElementInfo(ship, shipInfo) {
  if (!ship || !shipInfo) return;
  ship.dataset.shipPosition = String(Number(shipInfo.position || 0));
  const lines = ship.querySelectorAll(".ship-line");
  if (lines[0]) lines[0].textContent = `位置 ${shipTextPosition(Number(shipInfo.position || 0))}`;
  if (lines[1]) lines[1].textContent = shipStatusName(shipInfo.status || "");
}

function syncStateShipInfo(shipId, shipInfo) {
  if (!state?.ships || !shipInfo) return;
  const id = Number(shipId);
  const key = state.ships[id] ? id : String(id);
  const ship = state.ships[key];
  if (!ship) return;
  ship.position = Number(shipInfo.position || 0);
  ship.status = shipInfo.status || ship.status || "";
  ship.routeIndex = Number(shipInfo.routeIndex || ship.routeIndex || 1);
}

function redrawShipFromSnapshot(shipId, shipInfo) {
  const existing = shipElement(shipId);
  if (!existing || !shipInfo) return;
  const id = Number(shipId);
  const current = getShip(id) || {};
  const routeIndex = Number(shipInfo.routeIndex || current.routeIndex || 1);
  const ship = {
    ...current,
    shipId: Number(current.shipId || id),
    goodsId: Number(current.goodsId || id),
    position: Number(shipInfo.position || 0),
    status: shipInfo.status || current.status || "sailing",
    routeIndex,
  };
  const holder = document.createElement("div");
  holder.innerHTML = renderShip(ship, routeIndex - 1);
  const next = holder.firstElementChild;
  if (!next) return;
  existing.replaceWith(next);
  wireShipElement(next);
}

function finishShipMove(ship, endPoint) {
  if (!ship) return;
  ship.style.left = `${endPoint.x}%`;
  ship.style.top = `${endPoint.y}%`;
  ship.classList.remove("animating-move");
  ship.style.transition = "";
}

function showPirateCue(shipId) {
  const id = Number(shipId);
  queueAnimation("ship:group", (done) => {
    const ship = shipElement(id);
    if (!ship) {
      done();
      return null;
    }
    if (!animationsEnabled) {
      done();
      return null;
    }
    ship.classList.remove("pirate-warning");
    void ship.offsetWidth;
    ship.classList.add("pirate-warning");
    const icon = document.createElement("span");
    icon.className = "pirate-cue";
    icon.textContent = "☠";
    ship.appendChild(icon);
    const timer = setTimeout(() => {
      ship.classList.remove("pirate-warning");
      icon.remove();
      done();
    }, BOARD_ANIMATION_DURATION_MS);
    return {
      finish: () => {
        clearTimeout(timer);
        ship.classList.remove("pirate-warning");
        icon.remove();
      },
    };
  }, () => {
    const ship = shipElement(id);
    ship?.classList.remove("pirate-warning");
    ship?.querySelectorAll(".pirate-cue").forEach((item) => item.remove());
  });
}

function queuePirateBoardedPieceUpdate(data, sourceGame) {
  const shipId = data.shipId;
  const playerId = data.playerId;
  const seatIndex = shipSeatIndexFromEventData(data);
  const commit = () => syncShipSeatCell(shipId, playerId, sourceGame, seatIndex, pieceFromEventData(data));
  queueAnimation(`cell:ship:${shipId}:${seatIndex}`, (done) => {
    commit();
    const piece = shipSeatElement(shipId, seatIndex);
    if (!animationsEnabled) {
      done();
      return null;
    }
    if (!piece) {
      done();
      return null;
    }
    return startElementPulse(piece, "anim-piece-pulse", done);
  }, commit);
}

function startElementPulse(element, className, done) {
  let timer = null;
  let raf1 = null;
  let raf2 = null;
  const start = () => {
    element.classList.remove(className);
    void element.offsetWidth;
    element.classList.add(className);
    timer = setTimeout(() => {
      element.classList.remove(className);
      done();
    }, BOARD_ANIMATION_DURATION_MS);
  };
  if (typeof requestAnimationFrame === "function") {
    raf1 = requestAnimationFrame(() => {
      raf1 = null;
      raf2 = requestAnimationFrame(() => {
        raf2 = null;
        start();
      });
    });
  } else {
    start();
  }
  return {
    finish: () => {
      if (raf1 !== null) cancelAnimationFrame(raf1);
      if (raf2 !== null) cancelAnimationFrame(raf2);
      if (timer !== null) clearTimeout(timer);
      element.classList.remove(className);
    },
  };
}

function clearActiveAnimations(snapMoves) {
  animationQueues.clear();
  for (const key of [...activeAnimations.keys()]) {
    cancelActiveAnimation(key, snapMoves);
  }
  if (!hasActiveOrQueuedAnimations()) scheduleDeferredRenderFlush();
}

function cancelActiveAnimation(key, snapMoves) {
  const active = activeAnimations.get(key);
  if (!active) return null;
  activeAnimations.delete(key);
  active.finish?.(snapMoves);
  return active;
}

function queueAnimation(key, task, commit = null) {
  const queue = animationQueues.get(key) || [];
  const wasQueued = activeAnimations.has(key) || queue.length > 0;
  queue.push({ task, wasQueued, commit });
  animationQueues.set(key, queue);
  if (!activeAnimations.has(key)) runNextAnimation(key);
}

function runNextAnimation(key) {
  const queue = animationQueues.get(key);
  const item = queue?.shift();
  if (!item) {
    animationQueues.delete(key);
    if (!hasActiveOrQueuedAnimations()) scheduleDeferredRenderFlush();
    return;
  }
  const active = { finish: null };
  activeAnimations.set(key, active);
  const done = () => {
    if (activeAnimations.get(key) !== active) return;
    activeAnimations.delete(key);
    item.commit?.();
    runNextAnimation(key);
  };
  const cleanup = item.task(done, item.wasQueued);
  active.finish = cleanup?.finish || null;
}

function hasActiveOrQueuedAnimations() {
  if (activeAnimations.size > 0) return true;
  for (const queue of animationQueues.values()) {
    if (queue.length > 0) return true;
  }
  return false;
}

function scheduleDeferredRenderFlush() {
  if (!deferredRenderRoom || !deferredRenderAllowed || deferredRenderApplying) return;
  setTimeout(() => {
    flushDeferredRender().catch(showError);
  }, 0);
}

async function flushDeferredRender() {
  if (deferredRenderApplying || hasActiveOrQueuedAnimations() || !deferredRenderRoom || !deferredRenderAllowed) return;
  const nextRoom = deferredRenderRoom;
  clearDeferredRenderRoom();
  deferredRenderApplying = true;
  try {
    await applyRoomPayload(nextRoom, { animate: false });
  } finally {
    deferredRenderApplying = false;
  }
  if (deferredRenderRoom && !hasActiveOrQueuedAnimations()) scheduleDeferredRenderFlush();
}

function shipElement(shipId) {
  return document.querySelector(`.sea-overlay .ship[data-ship-id="${selectorValue(shipId)}"]`);
}

function selectorValue(value) {
  return String(value ?? "").replaceAll("\\", "\\\\").replaceAll('"', '\\"');
}

function selectedShipIdsForGame(game) {
  const fromRound = (game?.round?.selectedGoods || []).map(Number);
  if (fromRound.length) return fromRound;
  return Object.values(game?.ships || {}).map((ship) => Number(ship.shipId)).sort((a, b) => a - b);
}

function getShipFromGame(game, id) {
  return game?.ships?.[id] || game?.ships?.[String(id)];
}

function shipBoardPositionForAnimation(position, status) {
  const numericPosition = Number(position || 0);
  if (numericPosition > 13) return 14;
  if (status === "arrived" && numericPosition <= 0) return 14;
  return Math.max(0, Math.min(13, numericPosition));
}

function toggleAnimations() {
  animationsEnabled = !animationsEnabled;
  localStorage.setItem("manilaAnimationsEnabled", animationsEnabled ? "true" : "false");
  syncAnimationToggleButton();
  if (!animationsEnabled) clearActiveAnimations(true);
}

function syncAnimationToggleButton() {
  const button = $("animationToggleBtn");
  if (!button) return;
  button.textContent = animationsEnabled ? "动画 开" : "动画 关";
  button.setAttribute("aria-pressed", animationsEnabled ? "true" : "false");
}

function connectGameSocket() {
  if (!gameId || state?.status === "ended") return;
  clearTimeout(reconnectTimer);
  if (gameSocket) {
    gameSocket.onclose = null;
    gameSocket.close();
  }
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const url = `${protocol}//${window.location.host}/games/${encodeURIComponent(gameId)}/ws?token=${encodeURIComponent(roomToken || "")}`;
  gameSocket = new WebSocket(url);
  gameSocket.onmessage = (event) => {
    const data = JSON.parse(event.data || "{}");
    if (data.playerId !== undefined) localPlayerId = Number(data.playerId || 0);
    enqueueWSPayload(roomFromGamePayload(data));
  };
  gameSocket.onclose = () => {
    if (state?.status !== "ended") reconnectTimer = setTimeout(connectGameSocket, 1200);
  };
}

function renderRoom() {
  const app = document.querySelector(".app-shell");
  const inGame = room && state && room.status === "inProgress";
  if (app) app.hidden = false;
  if (inGame) {
    syncActionLockUI();
    render();
    setBusyButtons(false);
  } else {
    syncActionLockUI();
    renderEmpty();
  }
}

function renderPostActionControls() {
  if (!room || !state || room.status !== "inProgress") {
    renderRoom();
    return;
  }
  syncTransientControls();
  renderContextControls();
  renderActionList();
  setBusyButtons(false);
  syncActionLockUI();
  syncAnimationToggleButton();
}

async function loadLegalActions() {
  actions = [];
  actionEventSeq = state?.eventSeq ?? null;
  if (!state || state.status === "ended") {
    return actionEventSeq;
  }
  if (!getLocalPlayerId()) {
    return actionEventSeq;
  }
  const data = await api(`/games/${encodeURIComponent(gameId)}/actions`);
  const loadedActions = data.actions || [];
  if (data.playerId !== undefined) localPlayerId = Number(data.playerId || 0);
  actionEventSeq = data.eventSeq ?? actionEventSeq;
  if (actionInputLocked && !canUnlockLocalActions(loadedActions, actionEventSeq)) {
    actions = [];
    return actionEventSeq;
  }
  actions = loadedActions;
  if (actionInputLocked) unlockLocalActions();
  return actionEventSeq;
}

function canUnlockLocalActions(loadedActions, loadedEventSeq) {
  if (pendingSubmittedAction?.type === "AI_ROUND") {
    return canUnlockAIRoundLock();
  }
  if (!Array.isArray(loadedActions) || loadedActions.length === 0) return false;
  if (!isLocalActionWindow()) return false;
  if (!pendingSubmittedAction) return true;
  return Number(loadedEventSeq || 0) > Number(pendingSubmittedAction.expectedEventSeq || 0);
}

function canUnlockAIRoundLock() {
  if (!state || !pendingSubmittedAction) return false;
  if (state.status === "ended") return true;
  const targetRound = Number(pendingSubmittedAction.targetRoundNumber || pendingSubmittedAction.roundNumber || 0);
  const currentRound = Number(state.roundNumber || 0);
  if (!targetRound) return false;
  if (currentRound > targetRound) return true;
  return currentRound === targetRound && state.phase === "RoundReview";
}

function isLocalActionWindow() {
  return isLocalActionWindowForGame(state);
}

function isLocalActionWindowForGame(game) {
  if (!game || game.status === "ended") return false;
  const localPlayerId = getLocalPlayerId();
  if (localPlayerId <= 0) return false;
  if (game.phase === "RoundReview") {
    return !(game.round?.confirmedPlayers || {})[localPlayerId];
  }
  return expectedActorIdForGame(game) === localPlayerId;
}

function expectedActorIdForPhase() {
  return expectedActorIdForGame(state);
}

function expectedActorIdForGame(game) {
  if (!game) return 0;
  if (game.phase === "HarborMasterBuyShare" || game.phase === "HarborMasterSelectGoods" || game.phase === "HarborMasterSetShips") {
    return Number(game.harborMaster || game.currentPlayer || 0);
  }
  return Number(game.currentPlayer || 0);
}

function canLocalAIForGame(game) {
  if (!game || game.status === "ended" || !getLocalPlayerId()) return false;
  if (game.phase === "RoundReview") {
    return !(game.round?.confirmedPlayers || {})[getLocalPlayerId()];
  }
  return expectedActorIdForGame(game) === getLocalPlayerId();
}

function lockLocalActions(actionRequest) {
  actionInputLocked = true;
  pendingSubmittedAction = actionRequest || null;
  actions = [];
  actionEventSeq = null;
  resetTransientControls();
  hideBoardActionTargets();
  renderRoom();
}

function unlockLocalActions() {
  actionInputLocked = false;
  pendingSubmittedAction = null;
  syncActionLockUI();
}

function syncActionLockUI() {
  document.querySelector(".app-shell")?.classList.toggle("actions-locked", actionInputLocked);
}

function hideBoardActionTargets() {
  document.querySelectorAll(".board-stage [data-action-key], .top-dashboard [data-action-key]").forEach((el) => {
    el.classList.remove("actionable");
    el.removeAttribute("data-action-key");
    if (el.matches("button")) el.disabled = true;
  });
}

async function submitAction(action, payloadOverride) {
  if (actionInputLocked) return;
  if (!state || !action) return;
  const localPlayerId = getLocalPlayerId();
  const expectedActorId = expectedActorIdForPhase();
  if (state.phase !== "RoundReview" && expectedActorId !== localPlayerId) {
    showToast(`等待 P${expectedActorId || state.currentPlayer} 行动。`);
    return;
  }
  const payload = payloadOverride === undefined ? normalizePayload(action.payload || {}) : payloadOverride;
  const requestBody = {
    playerId: localPlayerId,
    type: action.type,
    payload,
    expectedEventSeq: actionEventSeq ?? state.eventSeq,
  };
  lockLocalActions(requestBody);
  try {
    await api(`/games/${encodeURIComponent(gameId)}/actions`, { method: "POST", body: JSON.stringify(requestBody) });
    resetTransientControls();
  } catch (err) {
    unlockLocalActions();
    await loadLegalActions().then(renderPostActionControls).catch(() => {});
    throw err;
  }
}

async function aiStep() {
  if (!canLocalAIForGame(state) || aiBusy || actionInputLocked) return;
  aiBusy = true;
  lockLocalActions({
    type: "AI_STEP",
    expectedEventSeq: state?.eventSeq ?? actionEventSeq ?? 0,
    roundNumber: state?.roundNumber ?? 0,
  });
  setBusyButtons(true);
  try {
    await api(`/games/${encodeURIComponent(gameId)}/ai-step`, { method: "POST" });
  } catch (err) {
    unlockLocalActions();
    await loadLegalActions().then(renderPostActionControls).catch(() => {});
    throw err;
  } finally {
    aiBusy = false;
    setBusyButtons(false);
  }
}

async function aiAdvanceRound() {
  if (!canLocalAIForGame(state) || aiBusy || actionInputLocked) return;
  aiBusy = true;
  const currentRound = Number(state?.roundNumber || 0);
  lockLocalActions({
    type: "AI_ROUND",
    expectedEventSeq: state?.eventSeq ?? actionEventSeq ?? 0,
    roundNumber: currentRound,
    targetRoundNumber: state?.phase === "RoundReview" ? currentRound + 1 : currentRound,
  });
  setBusyButtons(true);
  try {
    await api(`/games/${encodeURIComponent(gameId)}/ai-round`, { method: "POST" });
  } catch (err) {
    unlockLocalActions();
    await loadLegalActions().then(renderPostActionControls).catch(() => {});
    throw err;
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
  renderDestinationZones();
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
  actionEventSeq = null;
  for (const id of ["gameIdText", "roundText", "phaseText", "currentPlayerText", "harborMasterText"]) {
    $(id).textContent = "-";
  }
  $("goodsMarket").innerHTML = "";
  $("portSlots").innerHTML = "";
  $("dockSlots").innerHTML = "";
  $("destinationZones").innerHTML = "";
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
  const settlement = settlementForDisplay();
  const scores = settlement?.scores || [];
  if (!scores.length || settlementOverlayClosed) {
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
  overlay.querySelector(".settlement-close")?.addEventListener("click", closeSettlementOverlay);
}

function closeSettlementOverlay() {
  settlementOverlayClosed = true;
  renderSettlementOverlay();
}

function settlementForDisplay() {
  const scores = finalScoresForDisplay();
  if (!scores.length) return null;
  return {
    gameId: state?.gameId || "",
    eventSeq: state?.eventSeq || 0,
    scores,
  };
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
    const action = placementAction("port", slotId);
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
    const action = placementAction("dock", slotId);
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

function renderDestinationZones() {
  const dockAction = destinationAction("dock");
  const portAction = destinationAction("port");
  if (!dockAction && !portAction) {
    $("destinationZones").innerHTML = "";
    return;
  }
  $("destinationZones").innerHTML = [
    destinationZoneHTML("dock", "船坞", dockAction),
    destinationZoneHTML("port", "港口", portAction),
  ].join("");
  wireBoardSlots("destinationZones");
}

function destinationZoneHTML(kind, label, action) {
  const actionKey = action ? actionKeyFor(action) : "";
  const classes = ["destination-zone", kind, action ? "actionable" : ""].filter(Boolean).join(" ");
  return `<button class="${classes}" data-action-key="${escapeAttr(actionKey)}" type="button" aria-label="选择${label}"></button>`;
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
  if (hasActiveOrQueuedAnimations() && !forceAnimationBaseRender) return;
  const selected = selectedShipIds();
  if (selected.length === 0) {
    $("seaLanes").innerHTML = `<div class="empty-note">欢迎来到马尼拉！</div>`;
    return;
  }

  $("seaLanes").innerHTML = selected.map((shipId, index) => {
    const ship = getShip(shipId);
    return ship ? renderShip(ship, index) : "";
  }).join("");

  $("seaLanes").querySelectorAll(".ship[data-action-key]").forEach(wireShipElement);
}

function wireShipElement(el) {
  const action = actionByKey(el.dataset.actionKey);
  if (!action) return;
  el.addEventListener("click", () => submitAction(action).catch(showError));
}

function renderTopShips() {
  if (hasTopShipSeatAnimations() && !forceAnimationBaseRender) return;
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

function hasTopShipSeatAnimations() {
  for (const key of activeAnimations.keys()) {
    if (String(key).startsWith("cell:ship:")) return true;
  }
  for (const [key, queue] of animationQueues.entries()) {
    if (String(key).startsWith("cell:ship:") && queue.length > 0) return true;
  }
  return false;
}

function renderTopShip(ship) {
  const shipId = Number(ship.shipId);
  const meta = goodsMeta[shipId];
  if (!meta) return "";
  const occupants = ship.occupants || [];
  const slots = boardingCosts(shipId).map((cost, index) => {
    const occupant = occupants[index];
    const style = occupant ? pieceStyle(occupant) : "";
    const pieceAttrs = occupant ? `data-player-id="${Number(occupant.playerId)}" data-piece-role="${escapeAttr(occupant.role || "")}"` : "";
    return `<span class="top-ship-seat ${occupant ? "occupied" : ""}" data-ship-id="${shipId}" data-seat-index="${index}" ${pieceAttrs} style="${style}">${occupant ? `P${occupant.playerId}` : cost}</span>`;
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
  const basePosition = Number(ship.position || 0);
  const displayPosition = shipTextPosition(basePosition);
  const point = shipPoint(shipBoardPosition(basePosition, ship.status), routeIndex);
  const actionKey = action ? actionKeyFor(action) : "";
  const classes = ["ship", meta.className, action ? "actionable" : ""].filter(Boolean).join(" ");
  const icon = goodsIconSrc(shipId);
  return `<div class="${classes}" data-action-key="${escapeAttr(actionKey)}" data-ship-id="${shipId}" data-ship-position="${escapeAttr(basePosition)}" data-route-index="${routeIndex}" style="left: ${point.x}%; top: ${point.y}%;">
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
  if (hasSpecialSlotAnimations() && !forceAnimationBaseRender) return;
  const piratePieces = pirateSlots();
  $("pirateSlots").innerHTML = [
    specialSlotHTML("船长", "花费 5", piratePieces[0], placementAction("pirate", 1), slotLabels(5, null), "pirate", "1"),
    specialSlotHTML("副手", "花费 5", piratePieces[1], placementAction("pirate", 2), slotLabels(5, null), "pirate", "2"),
  ].join("");

  $("navigatorSlots").innerHTML = [
    specialSlotHTML("小领航", "花费 2", state.board?.smallNavigator, placementAction("navigatorSmall", "small"), slotLabels(2, null), "navigatorSmall", "small"),
    specialSlotHTML("大领航", "花费 5", state.board?.bigNavigator, placementAction("navigatorBig", "big"), slotLabels(5, null), "navigatorBig", "big"),
  ].join("");

  $("insuranceSlot").innerHTML = specialSlotHTML("保险商", "立即获得 10", state.board?.insurance, placementAction("insurance", "insurance"), [
    { type: "reward", text: "收益 10" },
    { type: "cost", text: "赔船坞" },
  ], "insurance", "insurance");

  for (const id of ["pirateSlots", "navigatorSlots", "insuranceSlot"]) {
    wireBoardSlots(id);
  }
}

function hasSpecialSlotAnimations() {
  for (const key of activeAnimations.keys()) {
    if (isSpecialSlotAnimationKey(key)) return true;
  }
  for (const [key, queue] of animationQueues.entries()) {
    if (isSpecialSlotAnimationKey(key) && queue.length > 0) return true;
  }
  return false;
}

function isSpecialSlotAnimationKey(key) {
  const value = String(key);
  return value.startsWith("cell:pirate:")
    || value.startsWith("cell:navigatorSmall:")
    || value.startsWith("cell:navigatorBig:")
    || value.startsWith("cell:insurance:");
}

function specialSlotHTML(title, meta, piece, action, labels, kind = "", slotId = "") {
  const actionKey = action ? actionKeyFor(action) : "";
  const classes = ["board-slot", piece ? "filled" : "", action ? "actionable" : ""].filter(Boolean).join(" ");
  const debugClass = DEBUG_SHOW_ALL_HOTSPOTS ? " debug-hotspot" : "";
  return `<div class="${classes}${debugClass}" data-action-key="${escapeAttr(actionKey)}" data-kind="${escapeAttr(kind)}" data-slot-id="${escapeAttr(slotId)}">
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


function playerDisplayName(playerId) {
  const seat = (room?.seats || []).find((item) => Number(item.playerId) === Number(playerId));
  if (seat?.type === "human" && seat.name) return seat.name;
  if (seat?.type === "ai") return `AI P${playerId}`;
  return `P${playerId}`;
}

function playerNameHTML(playerId) {
  const name = playerDisplayName(playerId);
  return `<span class="player-title">${pieceHTML({ playerId })}<span class="player-display-name" title="${escapeAttr(name)}">${escapeHTML(name)}</span></span>`;
}

function renderPlayers() {
  const players = playerOrder.map((id) => state.players?.[id]).filter(Boolean);
  $("playersList").innerHTML = players.map((player) => {
    const current = Number(state.currentPlayer) === Number(player.playerId);
    return `<article class="player-card ${current ? "current" : ""}">
      <div class="player-head">
        ${playerNameHTML(player.playerId)}
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
        ${playerNameHTML(player.playerId)}
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
      };
      input.onchange = () => {
        const value = clampNumberInput(input, 0, 5);
        shipStarts[input.dataset.shipId] = value;
        updateStartsSum();
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
  if (actionInputLocked) return undefined;
  return actions.find((a) => {
    if (a.type !== "PlaceAccomplice") return false;
    const payload = a.payload || {};
    return payload.positionType === positionType && String(payload.targetId) === String(targetId);
  });
}

function destinationAction(destination) {
  if (actionInputLocked) return undefined;
  return actions.find((a) => {
    return a.type === "PirateChooseDestination"
      && String(a.payload?.destination) === destination;
  });
}

function actionByKey(key) {
  if (actionInputLocked) return undefined;
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
    case "DiceRolled": return `第 ${data.movementStep} 次移动：${formatDiceResults(data.results)}`;
    case "ShipArrived": return `${goodsName(data.shipId)} 到港 ${data.slot}${hasValue(data.position) ? `，船位 ${data.position}` : ""}`;
    case "ShipDocked": return `${goodsName(data.shipId)} 进船坞 ${data.slot}${hasValue(data.position) ? `，船位 ${data.position}` : ""}`;
    case "PirateBoarded": return `P${data.playerId}${data.role ? `（${roleName(data.role)}）` : ""} 登上 ${goodsName(data.shipId)}${data.shipRole ? `，货船身份：${roleName(data.shipRole)}` : ""}`;
    case "PirateBoardSkipped": return `P${data.playerId}${data.role ? `（${roleName(data.role)}）` : ""} 放弃登船`;
    case "NavigatorMoved": return `P${data.playerId} ${navigatorName(data.navigator)}：${formatMoves(data.moves)}`;
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

function pieceHTML(piece) {
  if (!piece) return `<span class="piece empty">空</span>`;
  const boarded = piece.role === "boardedCaptain" || piece.role === "boardedMate";
  const label = boarded ? `P${piece.playerId}/已登船` : `P${piece.playerId}`;
  return `<span class="piece ${boarded ? "boarded" : ""}" data-player-id="${Number(piece.playerId)}" data-piece-role="${escapeAttr(piece.role || "")}" style="${pieceStyle(piece)}">${label}</span>`;
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
  const ended = state?.status === "ended";
  const disabled = ended || isBusy || actionInputLocked || !canLocalAIForGame(state);
  $("aiStepBtn").disabled = disabled;
  $("aiRoundBtn").disabled = disabled;
  $("returnLobbyBtn").disabled = false;
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

function openHelp(helpId) {
  const help = HELP_CONTENT[helpId];
  if (!help) return;
  $("helpTitle").textContent = help.title;
  $("helpBody").innerHTML = help.body.map((paragraph) => `<p>${escapeHTML(paragraph)}</p>`).join("");
  $("helpOverlay").hidden = false;
}

function closeHelp() {
  $("helpOverlay").hidden = true;
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function escapeAttr(value) {
  return String(value).replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;");
}

$("aiStepBtn").onclick = () => aiStep().catch(showError);
$("aiRoundBtn").onclick = () => aiAdvanceRound().catch(showError);
$("animationToggleBtn").onclick = toggleAnimations;
$("returnLobbyBtn").onclick = returnToLobby;
$("helpCloseBtn").onclick = closeHelp;
$("helpOverlay").addEventListener("click", (event) => {
  if (event.target === $("helpOverlay")) closeHelp();
});
document.addEventListener("click", (event) => {
  const button = event.target.closest(".help-button[data-help]");
  if (!button) return;
  event.preventDefault();
  event.stopPropagation();
  openHelp(button.dataset.help);
});
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") closeHelp();
});

syncAnimationToggleButton();
refresh().then(() => {
  if (state?.status !== "ended") connectGameSocket();
}).catch(showError);
