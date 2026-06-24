package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"manila/internal/app"
	"manila/internal/model"
	"manila/internal/rules"
	"manila/internal/store"
)

func TestStaticCacheHeaders(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	assertCacheHeader(t, mux, "/", "no-cache, must-revalidate")
	assertCacheHeader(t, mux, "/game.html", "no-cache, must-revalidate")
	assertCacheHeader(t, mux, "/assets/manila-board-base.png", "public, max-age=31536000, immutable")
	assertCacheHeader(t, mux, "/game.js?v=game-test", "public, max-age=31536000, immutable")
	assertCacheHeader(t, mux, "/board.css?v=board-test", "public, max-age=31536000, immutable")
	assertCacheHeader(t, mux, "/app.js", "no-cache, must-revalidate")
}

func TestHTTPCreateStartAndActions(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	body := bytes.NewBufferString(`{"seed":42}`)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/debug/games", body)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", res.Code, res.Body.String())
	}
	var created map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	game := created["game"].(map[string]interface{})
	gameID := game["gameId"].(string)

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/debug/games/"+gameID+"/start", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("start status %d body %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/debug/games/"+gameID+"/players/1/actions", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("actions status %d body %s", res.Code, res.Body.String())
	}
	var actions map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &actions); err != nil {
		t.Fatal(err)
	}
	if len(actions["actions"].([]interface{})) == 0 {
		t.Fatal("expected legal actions")
	}
}

func assertCacheHeader(t *testing.T, mux *http.ServeMux, path string, expected string) {
	t.Helper()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET %s status %d body %s", path, res.Code, res.Body.String())
	}
	if got := res.Header().Get("Cache-Control"); got != expected {
		t.Fatalf("GET %s Cache-Control = %q, want %q", path, got, expected)
	}
	if strings.Contains(expected, "immutable") && !strings.Contains(res.Header().Get("Cache-Control"), "max-age=31536000") {
		t.Fatalf("GET %s should be long cached, got %q", path, res.Header().Get("Cache-Control"))
	}
}

func TestHTTPTrainingRandomGames(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/training/random-games", bytes.NewBufferString(`{"count":2,"seed":3,"maxSteps":120}`))
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("training status %d body %s", res.Code, res.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload["games"].([]interface{})) != 2 {
		t.Fatalf("expected two game summaries, got %s", res.Body.String())
	}
}

func TestHTTPTrainedAI(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/debug/games", bytes.NewBufferString(`{"seed":88}`))
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", res.Code, res.Body.String())
	}
	var created map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	gameID := created["game"].(map[string]interface{})["gameId"].(string)

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/debug/games/"+gameID+"/start", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("start status %d body %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/debug/games/"+gameID+"/players/1/trained-ai", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("trained ai status %d body %s", res.Code, res.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["action"] == nil {
		t.Fatalf("expected action in response: %s", res.Body.String())
	}
}

func TestRoomJoinSeatsStartAndObserverVisibility(t *testing.T) {
	st := store.NewMemoryStore()
	svc := app.NewService(st, rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	token := roomJoinNamed(t, mux, "Alice")
	roomRequest(t, mux, http.MethodPost, "/room/seats/1/claim", token, nil)
	roomRequest(t, mux, http.MethodPost, "/room/seats/2/ai", token, nil)
	roomRequest(t, mux, http.MethodPost, "/room/seats/3/ai", token, nil)
	roomRequest(t, mux, http.MethodPost, "/room/seats/4/ai", token, nil)
	started := roomRequest(t, mux, http.MethodPost, "/room/ready", token, nil)
	room := started["room"].(map[string]interface{})
	if room["status"] != "inProgress" {
		t.Fatalf("expected inProgress room, got %s", started)
	}
	if room["game"] != nil {
		t.Fatalf("room view should not include game payload: %s", started)
	}
	gameID := room["gameId"].(string)
	ownState := roomRequest(t, mux, http.MethodGet, "/games/"+gameID+"/state", token, nil)
	if _, ok := ownState["timeout"]; ok {
		t.Fatalf("single-human game state should not include action timeout, got %s", ownState)
	}
	stateSeats := ownState["seats"].([]interface{})
	if stateSeats[0].(map[string]interface{})["name"] != "Alice" {
		t.Fatalf("game state should include room seat names, got %s", ownState)
	}
	ownGame := ownState["game"].(map[string]interface{})
	ownPlayers := ownGame["players"].(map[string]interface{})
	ownP1 := ownPlayers["1"].(map[string]interface{})
	if _, ok := ownP1["shares"].(map[string]interface{}); !ok {
		t.Fatalf("seated token should see own shares: %s", started)
	}

	observer := roomRequest(t, mux, http.MethodGet, "/games/"+gameID+"/state", "", nil)
	observerGame := observer["game"].(map[string]interface{})
	observerPlayers := observerGame["players"].(map[string]interface{})
	observerP1 := observerPlayers["1"].(map[string]interface{})
	if hidden, ok := observerP1["hiddenShareCount"].(float64); !ok || hidden == 0 {
		t.Fatalf("observer should see hidden share count for P1, got %s", observer)
	}
	shares := observerP1["shares"].(map[string]interface{})
	if len(shares) != 0 {
		t.Fatalf("observer should not see initial private shares, got %v", shares)
	}

	forbidden := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/games/"+gameID+"/actions", nil)
	mux.ServeHTTP(forbidden, req)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected unauthenticated production actions to fail, status %d body %s", forbidden.Code, forbidden.Body.String())
	}

	allowed := roomRequest(t, mux, http.MethodGet, "/games/"+gameID+"/actions", token, nil)
	if len(allowed["actions"].([]interface{})) == 0 {
		t.Fatalf("expected seated token to receive actions, got %s", allowed)
	}

	ref, ok := st.Get(gameID)
	if !ok {
		t.Fatalf("expected game %s in store", gameID)
	}
	ref.Mu.Lock()
	ref.Game.Status = model.StatusEnded
	endedGame := ref.Game
	ref.Mu.Unlock()
	svc.FinishActiveRoomGame(endedGame)
	endedState := roomRequest(t, mux, http.MethodGet, "/games/"+gameID+"/state", token, nil)
	endedSeats := endedState["seats"].([]interface{})
	if endedSeats[0].(map[string]interface{})["name"] != "Alice" {
		t.Fatalf("ended game state should keep room seat names, got %s", endedState)
	}
	roomRequest(t, mux, http.MethodPatch, "/lobby/name", token, bytes.NewBufferString(`{"name":"Carol"}`))
	renamedState := roomRequest(t, mux, http.MethodGet, "/games/"+gameID+"/state", token, nil)
	renamedSeats := renamedState["seats"].([]interface{})
	if renamedSeats[0].(map[string]interface{})["name"] != "Carol" {
		t.Fatalf("ended game state should resolve current participant names by token, got %s", renamedState)
	}
}

func TestMultiplayerGameStateIncludesTimeoutCountdown(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	alice := roomJoinNamed(t, mux, "Alice")
	bob := roomJoinNamed(t, mux, "Bob")
	roomRequest(t, mux, http.MethodPost, "/room/seats/1/claim", alice, nil)
	roomRequest(t, mux, http.MethodPost, "/room/seats/2/claim", bob, nil)
	roomRequest(t, mux, http.MethodPost, "/room/seats/3/ai", alice, nil)
	roomRequest(t, mux, http.MethodPost, "/room/seats/4/ai", alice, nil)
	roomRequest(t, mux, http.MethodPost, "/room/ready", alice, nil)
	started := roomRequest(t, mux, http.MethodPost, "/room/ready", bob, nil)
	gameID := started["room"].(map[string]interface{})["gameId"].(string)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state := roomRequest(t, mux, http.MethodGet, "/games/"+gameID+"/state", alice, nil)
		timeoutPayload, ok := state["timeout"].(map[string]interface{})
		if !ok {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if timeoutPayload["kind"] != "action" {
			t.Fatalf("timeout kind = %v, want action in %s", timeoutPayload["kind"], state)
		}
		if got := int(timeoutPayload["durationSeconds"].(float64)); got != 60 {
			t.Fatalf("durationSeconds = %d, want 60 in %s", got, state)
		}
		remaining := int(timeoutPayload["remainingSeconds"].(float64))
		if remaining <= 0 || remaining > 60 {
			t.Fatalf("remainingSeconds = %d, want 1..60 in %s", remaining, state)
		}
		remainingMillis := int(timeoutPayload["remainingMillis"].(float64))
		if remainingMillis <= 0 || remainingMillis > 60000 {
			t.Fatalf("remainingMillis = %d, want 1..60000 in %s", remainingMillis, state)
		}
		if got := int(timeoutPayload["humanPlayerCount"].(float64)); got != 2 {
			t.Fatalf("humanPlayerCount = %d, want 2 in %s", got, state)
		}
		return
	}
	t.Fatalf("expected timeout countdown in multiplayer game state")
}

func TestGameActionEndpointTriggersRoomAutomation(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	token := roomJoinNamed(t, mux, "Alice")
	roomRequest(t, mux, http.MethodPost, "/room/seats/1/claim", token, nil)
	roomRequest(t, mux, http.MethodPost, "/room/seats/2/ai", token, nil)
	roomRequest(t, mux, http.MethodPost, "/room/seats/3/ai", token, nil)
	roomRequest(t, mux, http.MethodPost, "/room/seats/4/ai", token, nil)
	started := roomRequest(t, mux, http.MethodPost, "/room/ready", token, nil)
	gameID := started["room"].(map[string]interface{})["gameId"].(string)

	allowed := roomRequest(t, mux, http.MethodGet, "/games/"+gameID+"/actions", token, nil)
	seq := int(allowed["eventSeq"].(float64))
	body := bytes.NewBufferString(`{"playerId":1,"type":"PassBid","expectedEventSeq":` + strconv.Itoa(seq) + `}`)
	posted := roomRequest(t, mux, http.MethodPost, "/games/"+gameID+"/actions", token, body)
	postedSeq := int(posted["eventSeq"].(float64))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state := roomRequest(t, mux, http.MethodGet, "/games/"+gameID+"/state", token, nil)
		game := state["game"].(map[string]interface{})
		if int(game["eventSeq"].(float64)) > postedSeq {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected room automation to advance after game action endpoint, eventSeq remained %d", postedSeq)
}

func TestLobbyCreateRoomRequiresGlobalToken(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	state := roomRequest(t, mux, http.MethodGet, "/lobby/state", "", nil)
	lobby := state["lobby"].(map[string]interface{})
	participant := lobby["participant"].(map[string]interface{})
	if participant["joined"] == true {
		t.Fatalf("unauthenticated lobby view should not be joined: %s", state)
	}

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/rooms", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected room creation without token to fail, status %d body %s", res.Code, res.Body.String())
	}

	token := lobbyJoinNamed(t, mux, "Lobby Alice")
	created := roomRequest(t, mux, http.MethodPost, "/rooms", token, nil)
	if created["roomId"] == "" {
		t.Fatalf("expected created room id: %s", created)
	}
	room := created["room"].(map[string]interface{})
	if room["roomId"] != created["roomId"] {
		t.Fatalf("expected created room payload to include room id: %s", created)
	}
	participant = room["participant"].(map[string]interface{})
	if playerID, ok := participant["playerId"].(float64); ok && playerID != 0 {
		t.Fatalf("creator should enter room without taking a seat: %s", created)
	}
}

func TestRoomScopedSeatsAndGameActions(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	token := lobbyJoinNamed(t, mux, "Scoped Alice")
	created := roomRequest(t, mux, http.MethodPost, "/rooms", token, nil)
	roomID := created["roomId"].(string)
	base := "/rooms/" + roomID

	roomRequest(t, mux, http.MethodPost, base+"/seats/1/claim", token, nil)
	roomRequest(t, mux, http.MethodPost, base+"/seats/2/ai", token, nil)
	roomRequest(t, mux, http.MethodPost, base+"/seats/3/ai", token, nil)
	roomRequest(t, mux, http.MethodPost, base+"/seats/4/ai", token, nil)
	started := roomRequest(t, mux, http.MethodPost, base+"/ready", token, nil)
	room := started["room"].(map[string]interface{})
	if room["status"] != "inProgress" {
		t.Fatalf("expected scoped room to start, got %s", started)
	}
	gameID := room["gameId"].(string)

	actions := roomRequest(t, mux, http.MethodGet, "/games/"+gameID+"/actions", token, nil)
	if len(actions["actions"].([]interface{})) == 0 {
		t.Fatalf("expected game actions to resolve through room mapping, got %s", actions)
	}
}

func TestMultipleRoomsKeepSeatsIsolated(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	alice := lobbyJoinNamed(t, mux, "Multi Alice")
	bob := lobbyJoinNamed(t, mux, "Multi Bob")
	aliceRoom := roomRequest(t, mux, http.MethodPost, "/rooms", alice, nil)["roomId"].(string)
	bobRoom := roomRequest(t, mux, http.MethodPost, "/rooms", bob, nil)["roomId"].(string)

	roomRequest(t, mux, http.MethodPost, "/rooms/"+aliceRoom+"/seats/1/claim", alice, nil)
	roomRequest(t, mux, http.MethodPost, "/rooms/"+bobRoom+"/seats/1/claim", bob, nil)

	aliceView := roomRequest(t, mux, http.MethodGet, "/rooms/"+aliceRoom+"/state", alice, nil)["room"].(map[string]interface{})
	bobView := roomRequest(t, mux, http.MethodGet, "/rooms/"+bobRoom+"/state", bob, nil)["room"].(map[string]interface{})
	aliceSeats := aliceView["seats"].([]interface{})
	bobSeats := bobView["seats"].([]interface{})
	if !aliceSeats[0].(map[string]interface{})["isYou"].(bool) {
		t.Fatalf("expected Alice to own P1 in her room: %s", aliceView)
	}
	if !bobSeats[0].(map[string]interface{})["isYou"].(bool) {
		t.Fatalf("expected Bob to own P1 in his room: %s", bobView)
	}
}

func TestAdminCloseRoomRemovesRoomAndStopsActions(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	token := lobbyJoinNamed(t, mux, "Admin Target")
	created := roomRequest(t, mux, http.MethodPost, "/rooms", token, nil)
	roomID := created["roomId"].(string)
	base := "/rooms/" + roomID
	roomRequest(t, mux, http.MethodPost, base+"/seats/1/claim", token, nil)
	roomRequest(t, mux, http.MethodPost, base+"/seats/2/ai", token, nil)
	roomRequest(t, mux, http.MethodPost, base+"/seats/3/ai", token, nil)
	roomRequest(t, mux, http.MethodPost, base+"/seats/4/ai", token, nil)
	started := roomRequest(t, mux, http.MethodPost, base+"/ready", token, nil)
	gameID := started["room"].(map[string]interface{})["gameId"].(string)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, base+"/admin/close", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected player admin close to fail, status %d body %s", res.Code, res.Body.String())
	}

	roomRequest(t, mux, http.MethodPost, base+"/admin/close", svc.AdminToken(), nil)
	lobbyPayload := roomRequest(t, mux, http.MethodGet, "/lobby/state", svc.AdminToken(), nil)
	rooms := lobbyPayload["lobby"].(map[string]interface{})["rooms"].([]interface{})
	for _, item := range rooms {
		if item.(map[string]interface{})["roomId"] == roomID {
			t.Fatalf("closed room should be absent from lobby: %s", lobbyPayload)
		}
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/games/"+gameID+"/actions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected closed room actions to be forbidden, status %d body %s", res.Code, res.Body.String())
	}
	body := bytes.NewBufferString(`{"playerId":1,"type":"PassBid"}`)
	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/games/"+gameID+"/actions", body)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected closed room action to fail, status %d body %s", res.Code, res.Body.String())
	}
}

func TestRoomJoinRejectsDuplicateNames(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	_ = roomJoinNamed(t, mux, "Alice")
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/room/join", bytes.NewBufferString(`{"name":"Alice"}`))
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected duplicate name to fail, status %d body %s", res.Code, res.Body.String())
	}
}

func TestRoomJoinRejectsLongNames(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	roomJoinNamed(t, mux, "123456789012")

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/room/join", bytes.NewBufferString(`{"name":"1234567890123"}`))
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected long name to fail, status %d body %s", res.Code, res.Body.String())
	}
}

func TestRoomRenameKeepsTokenAndRejectsDuplicateNames(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	alice := roomJoinNamed(t, mux, "Alice")
	bob := roomJoinNamed(t, mux, "Bob")

	renamed := roomRequest(t, mux, http.MethodPatch, "/room/name", alice, bytes.NewBufferString(`{"name":"Carol"}`))
	if _, ok := renamed["token"]; ok {
		t.Fatalf("rename should not issue a new token, got %s", renamed)
	}
	roomPayload := renamed["room"].(map[string]interface{})
	participant := roomPayload["participant"].(map[string]interface{})
	if participant["name"] != "Carol" {
		t.Fatalf("expected renamed participant Carol, got %s", renamed)
	}

	sameName := roomRequest(t, mux, http.MethodPatch, "/room/name", alice, bytes.NewBufferString(`{"name":"Carol"}`))
	roomPayload = sameName["room"].(map[string]interface{})
	participant = roomPayload["participant"].(map[string]interface{})
	if participant["name"] != "Carol" {
		t.Fatalf("expected same-name rename to remain Carol, got %s", sameName)
	}

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/room/name", bytes.NewBufferString(`{"name":"Carol"}`))
	req.Header.Set("Authorization", "Bearer "+bob)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected duplicate rename to fail, status %d body %s", res.Code, res.Body.String())
	}
}

func roomJoin(t *testing.T, mux *http.ServeMux) string {
	t.Helper()
	return roomJoinNamed(t, mux, "")
}

func roomJoinNamed(t *testing.T, mux *http.ServeMux, name string) string {
	t.Helper()
	body := bytes.NewBufferString(`{}`)
	if name != "" {
		body = bytes.NewBufferString(`{"name":"` + name + `"}`)
	}
	payload := roomRequest(t, mux, http.MethodPost, "/room/join", "", body)
	token, ok := payload["token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected token in %s", payload)
	}
	return token
}

func lobbyJoinNamed(t *testing.T, mux *http.ServeMux, name string) string {
	t.Helper()
	body := bytes.NewBufferString(`{}`)
	if name != "" {
		body = bytes.NewBufferString(`{"name":"` + name + `"}`)
	}
	payload := roomRequest(t, mux, http.MethodPost, "/lobby/join", "", body)
	token, ok := payload["token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected token in %s", payload)
	}
	return token
}

func roomRequest(t *testing.T, mux *http.ServeMux, method string, path string, token string, body *bytes.Buffer) map[string]interface{} {
	t.Helper()
	if body == nil {
		body = bytes.NewBuffer(nil)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	mux.ServeHTTP(res, req)
	if res.Code < 200 || res.Code >= 300 {
		t.Fatalf("%s %s status %d body %s", method, path, res.Code, res.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
