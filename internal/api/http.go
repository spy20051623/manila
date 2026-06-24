package api

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"manila/internal/ai"
	"manila/internal/app"
	"manila/internal/model"
	"manila/internal/training"
)

//go:embed static/*
var staticFiles embed.FS

type Handler struct {
	service   *app.Service
	clientsMu sync.Mutex
	clients   map[*websocket.Conn]string
	upgrader  websocket.Upgrader
}

func NewHandler(s *app.Service) *Handler {
	h := &Handler{
		service: s,
		clients: map[*websocket.Conn]string{},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	s.SetNotifier(h.broadcastRoom)
	return h
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("/", staticFileServer())
	mux.HandleFunc("/room", h.handleRoom)
	mux.HandleFunc("/room/", h.handleRoomPath)
	mux.HandleFunc("/games", h.handleGames)
	mux.HandleFunc("/games/", h.handleGamePath)
	mux.HandleFunc("/debug/games/", h.handleDebugPath)
	mux.HandleFunc("/training/random-games", h.handleTrainingRandomGames)
	mux.HandleFunc("/training/evaluate-genome", h.handleTrainingEvaluateGenome)
	mux.HandleFunc("/training/train", h.handleTrainingTrain)
}

func staticFileServer() http.Handler {
	if _, err := os.Stat("internal/api/static"); err == nil {
		return noCache(remapStaticPages(http.FileServer(http.Dir("internal/api/static"))))
	}
	static, _ := fs.Sub(staticFiles, "static")
	return noCache(remapStaticPages(http.FileServer(http.FS(static))))
}

func remapStaticPages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/debug", "/debug/", "/debug.html":
			r = requestWithPath(r, "/debug.html")
		}
		next.ServeHTTP(w, r)
	})
}

func requestWithPath(r *http.Request, path string) *http.Request {
	cloned := r.Clone(r.Context())
	cloned.URL.Path = path
	cloned.URL.RawPath = ""
	return cloned
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleGames(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/games" || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "notFound", "not found")
		return
	}
	var req struct {
		Seed *int64 `json:"seed,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	g := h.service.CreateGame(req.Seed)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"game": g, "eventSeq": g.EventSeq})
}

func (h *Handler) handleRoom(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.respondRoom(w, r, http.StatusOK)
	case http.MethodPost:
		writeError(w, http.StatusNotFound, "notFound", "not found")
	default:
		writeError(w, http.StatusMethodNotAllowed, "methodNotAllowed", "method not allowed")
	}
}

func (h *Handler) handleRoomPath(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "room" {
		writeError(w, http.StatusNotFound, "notFound", "not found")
		return
	}
	token := tokenFromRequest(r)
	switch {
	case len(parts) == 2 && parts[1] == "state" && r.Method == http.MethodGet:
		h.respondRoom(w, r, http.StatusOK)
	case len(parts) == 2 && parts[1] == "join" && r.Method == http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		issued, err := h.service.JoinRoom(req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		view := h.roomViewForToken(issued)
		writeJSON(w, http.StatusOK, map[string]interface{}{"token": issued, "room": view})
	case len(parts) == 2 && parts[1] == "name" && r.Method == http.MethodPatch:
		var req struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		h.respondRoomCommand(w, r, h.service.RenameRoomParticipant(token, req.Name))
	case len(parts) == 2 && parts[1] == "ready" && r.Method == http.MethodPost:
		h.respondRoomCommand(w, r, h.service.Ready(token))
	case len(parts) == 2 && parts[1] == "unready" && r.Method == http.MethodPost:
		h.respondRoomCommand(w, r, h.service.Unready(token))
	case len(parts) == 2 && parts[1] == "actions" && r.Method == http.MethodGet:
		actions, seq, err := h.service.RoomLegalActions(token)
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"actions": actions, "eventSeq": seq})
	case len(parts) == 2 && parts[1] == "actions" && r.Method == http.MethodPost:
		var action model.Action
		if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
			writeError(w, http.StatusBadRequest, "badJson", err.Error())
			return
		}
		g, err := h.service.ApplyRoomAction(token, action)
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"game": h.visibleGameForToken(token, g), "eventSeq": g.EventSeq})
	case len(parts) == 2 && parts[1] == "ai-step" && r.Method == http.MethodPost:
		g, action, err := h.service.RoomAIStep(token)
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"game": h.visibleGameForToken(token, g), "action": action, "eventSeq": g.EventSeq})
	case len(parts) == 2 && parts[1] == "ai-round" && r.Method == http.MethodPost:
		g, err := h.service.RoomAIRound(token)
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"game": h.visibleGameForToken(token, g), "eventSeq": g.EventSeq})
	case len(parts) == 2 && parts[1] == "ws" && r.Method == http.MethodGet:
		h.handleRoomWS(w, r)
	case len(parts) == 4 && parts[1] == "seats" && parts[3] == "claim" && r.Method == http.MethodPost:
		playerID, err := strconv.Atoi(parts[2])
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", "invalid player id")
			return
		}
		h.respondRoomCommand(w, r, h.service.ClaimSeat(token, playerID))
	case len(parts) == 4 && parts[1] == "seats" && parts[3] == "leave" && r.Method == http.MethodPost:
		h.respondRoomCommand(w, r, h.service.LeaveSeat(token))
	case len(parts) == 4 && parts[1] == "seats" && parts[3] == "ai" && r.Method == http.MethodPost:
		playerID, err := strconv.Atoi(parts[2])
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", "invalid player id")
			return
		}
		h.respondRoomCommand(w, r, h.service.PlaceAI(token, playerID))
	case len(parts) == 4 && parts[1] == "seats" && parts[3] == "ai" && r.Method == http.MethodDelete:
		playerID, err := strconv.Atoi(parts[2])
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", "invalid player id")
			return
		}
		h.respondRoomCommand(w, r, h.service.RemoveAI(token, playerID))
	default:
		writeError(w, http.StatusNotFound, "notFound", "not found")
	}
}

func (h *Handler) respondRoomCommand(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	h.respondRoom(w, r, http.StatusOK)
}

func (h *Handler) respondRoom(w http.ResponseWriter, r *http.Request, status int) {
	writeJSON(w, status, map[string]interface{}{"room": h.roomViewForToken(tokenFromRequest(r))})
}

func (h *Handler) roomViewForToken(token string) model.RoomView {
	view, err := h.service.RoomView(token)
	if err != nil {
		return model.RoomView{Status: model.RoomStatusWaiting}
	}
	view.Game = h.visibleGameForToken(token, view.Game)
	if view.Game != nil {
		view.EventSeq = view.Game.EventSeq
	}
	return view
}

func (h *Handler) visibleGameForToken(token string, g *model.Game) *model.Game {
	return gameVisibleToViewer(g, h.service.RoomPlayerID(token))
}

func (h *Handler) handleRoomWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	connected := h.service.ConnectRoomParticipant(token)
	h.clientsMu.Lock()
	h.clients[conn] = token
	h.clientsMu.Unlock()
	_ = conn.WriteJSON(map[string]interface{}{"room": h.roomViewForToken(token)})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	h.clientsMu.Lock()
	delete(h.clients, conn)
	h.clientsMu.Unlock()
	_ = conn.Close()
	if connected {
		h.service.DisconnectRoomParticipant(token)
	}
}

func (h *Handler) broadcastRoom() {
	h.clientsMu.Lock()
	clients := map[*websocket.Conn]string{}
	for conn, token := range h.clients {
		clients[conn] = token
	}
	h.clientsMu.Unlock()
	for conn, token := range clients {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(map[string]interface{}{"room": h.roomViewForToken(token)}); err != nil {
			h.clientsMu.Lock()
			delete(h.clients, conn)
			h.clientsMu.Unlock()
			_ = conn.Close()
		}
	}
}

func (h *Handler) handleGamePath(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "games" {
		writeError(w, http.StatusNotFound, "notFound", "not found")
		return
	}
	gameID := parts[1]
	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "methodNotAllowed", "method not allowed")
			return
		}
		g, err := h.service.GetGame(gameID)
		respondGameForRequest(w, r, g, err)
		return
	}
	switch {
	case len(parts) == 3 && parts[2] == "start" && r.Method == http.MethodPost:
		g, err := h.service.StartGame(gameID)
		respondGameForRequest(w, r, g, err)
	case len(parts) == 3 && parts[2] == "state" && r.Method == http.MethodGet:
		g, err := h.service.GetGame(gameID)
		respondGameForRequest(w, r, g, err)
	case len(parts) == 3 && parts[2] == "events" && r.Method == http.MethodGet:
		events, seq, err := h.service.Events(gameID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"events": events, "eventSeq": seq})
	case len(parts) == 5 && parts[2] == "players" && parts[4] == "actions" && r.Method == http.MethodGet:
		playerID, err := strconv.Atoi(parts[3])
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", "invalid player id")
			return
		}
		if viewerID := viewerIDFromRequest(r); viewerID != 0 && viewerID != playerID {
			writeError(w, http.StatusForbidden, "forbidden", "viewer cannot inspect another player's actions")
			return
		}
		actions, seq, err := h.service.LegalActions(gameID, playerID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"actions": actions, "eventSeq": seq})
	case len(parts) == 3 && parts[2] == "actions" && r.Method == http.MethodPost:
		var action model.Action
		if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
			writeError(w, http.StatusBadRequest, "badJson", err.Error())
			return
		}
		if viewerID := viewerIDFromRequest(r); viewerID != 0 && viewerID != action.PlayerID {
			writeError(w, http.StatusForbidden, "forbidden", "viewer cannot act for another player")
			return
		}
		g, err := h.service.ApplyAction(gameID, action)
		respondGameForRequest(w, r, g, err)
	case len(parts) == 5 && parts[2] == "players" && parts[4] == "random-ai" && r.Method == http.MethodPost:
		playerID, err := strconv.Atoi(parts[3])
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", "invalid player id")
			return
		}
		g, action, err := h.service.ApplyRandomAI(gameID, playerID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"game": visibleGameForRequest(r, g), "action": action, "eventSeq": g.EventSeq})
	case len(parts) == 5 && parts[2] == "players" && parts[4] == "trained-ai" && r.Method == http.MethodPost:
		playerID, err := strconv.Atoi(parts[3])
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", "invalid player id")
			return
		}
		g, action, err := h.service.ApplyTrainedAI(gameID, playerID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"game": visibleGameForRequest(r, g), "action": action, "eventSeq": g.EventSeq})
	default:
		writeError(w, http.StatusNotFound, "notFound", "not found")
	}
}

func (h *Handler) handleDebugPath(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "debug" || parts[1] != "games" {
		writeError(w, http.StatusNotFound, "notFound", "not found")
		return
	}
	gameID := parts[2]
	switch parts[3] {
	case "snapshot":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "methodNotAllowed", "method not allowed")
			return
		}
		g, err := h.service.GetGame(gameID)
		respondGame(w, g, err)
	case "reset":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "methodNotAllowed", "method not allowed")
			return
		}
		var req struct {
			Seed *int64 `json:"seed,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		g, err := h.service.ResetGame(gameID, req.Seed)
		respondGame(w, g, err)
	case "set-seed":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "methodNotAllowed", "method not allowed")
			return
		}
		var req struct {
			Seed int64 `json:"seed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "badJson", err.Error())
			return
		}
		g, err := h.service.SetSeed(gameID, req.Seed)
		respondGame(w, g, err)
	default:
		writeError(w, http.StatusNotFound, "notFound", "not found")
	}
}

func (h *Handler) handleTrainingRandomGames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "methodNotAllowed", "method not allowed")
		return
	}
	var req struct {
		Count    int   `json:"count"`
		Seed     int64 `json:"seed"`
		MaxSteps int   `json:"maxSteps"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Seed == 0 {
		req.Seed = 1
	}
	result := training.RunRandomGames(req.Count, req.Seed, req.MaxSteps)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleTrainingEvaluateGenome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "methodNotAllowed", "method not allowed")
		return
	}
	var req struct {
		Genome         ai.Genome   `json:"genome"`
		SeedsPerSeat   int         `json:"seedsPerSeat"`
		MaxSteps       int         `json:"maxSteps"`
		BaseSeed       int64       `json:"baseSeed"`
		OpponentMode   string      `json:"opponentMode,omitempty"`
		BaselineGenome *ai.Genome  `json:"baselineGenome,omitempty"`
		BaselinePool   []ai.Genome `json:"baselinePool,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "badJson", err.Error())
		return
	}
	result := training.EvaluateGenome(req.Genome, training.EvaluationConfig{
		SeedsPerSeat:   req.SeedsPerSeat,
		MaxSteps:       req.MaxSteps,
		BaseSeed:       req.BaseSeed,
		OpponentMode:   req.OpponentMode,
		BaselineGenome: req.BaselineGenome,
		BaselinePool:   req.BaselinePool,
	})
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleTrainingTrain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "methodNotAllowed", "method not allowed")
		return
	}
	var cfg training.TrainConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "badJson", err.Error())
		return
	}
	result, err := training.TrainPopulation(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func respondGame(w http.ResponseWriter, g *model.Game, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"game": g, "eventSeq": g.EventSeq})
}

func respondGameForRequest(w http.ResponseWriter, r *http.Request, g *model.Game, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	visible := visibleGameForRequest(r, g)
	writeJSON(w, http.StatusOK, map[string]interface{}{"game": visible, "eventSeq": visible.EventSeq})
}

func visibleGameForRequest(r *http.Request, g *model.Game) *model.Game {
	viewerID := viewerIDFromRequest(r)
	return gameVisibleToViewer(g, viewerID)
}

func viewerIDFromRequest(r *http.Request) int {
	for _, key := range []string{"viewerId", "playerId"} {
		raw := r.URL.Query().Get(key)
		if raw == "" {
			continue
		}
		id, err := strconv.Atoi(raw)
		if err == nil && id >= 1 && id <= 4 {
			return id
		}
	}
	return 0
}

func gameVisibleToViewer(g *model.Game, viewerID int) *model.Game {
	if g == nil {
		return nil
	}
	data, err := json.Marshal(g)
	if err != nil {
		return g
	}
	var visible model.Game
	if err := json.Unmarshal(data, &visible); err != nil {
		return g
	}
	publicBuys := publicBoughtShares(visible.Events)
	for playerID, player := range visible.Players {
		if viewerID != 0 && playerID == viewerID {
			player.HiddenShareCount = 0
			continue
		}
		totalShares := 0
		for _, count := range player.Shares {
			totalShares += count
		}
		knownShares := publicBuys[playerID]
		knownTotal := 0
		filteredShares := map[model.GoodsID]int{}
		for goodsID, count := range knownShares {
			filteredShares[goodsID] = count
			knownTotal += count
		}
		player.Shares = filteredShares
		player.HiddenShareCount = maxInt(0, totalShares-knownTotal)
	}
	return &visible
}

func tokenFromRequest(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return r.URL.Query().Get("token")
}

func publicBoughtShares(events []model.Event) map[int]map[model.GoodsID]int {
	out := map[int]map[model.GoodsID]int{}
	for _, event := range events {
		if event.Type != "ShareBought" || event.Data == nil {
			continue
		}
		playerID, ok := eventDataInt(event.Data["playerId"])
		if !ok {
			continue
		}
		goodsIDValue, ok := eventDataInt(event.Data["goodsId"])
		if !ok {
			continue
		}
		if out[playerID] == nil {
			out[playerID] = map[model.GoodsID]int{}
		}
		out[playerID][model.GoodsID(goodsIDValue)]++
	}
	return out
}

func eventDataInt(v interface{}) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case model.GoodsID:
		return int(t), true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case json.Number:
		i, err := t.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]interface{}{"errorCode": code, "message": message})
}
