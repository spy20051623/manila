package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"manila/internal/app"
	"manila/internal/rules"
	"manila/internal/store"
)

func TestHTTPCreateStartAndActions(t *testing.T) {
	svc := app.NewService(store.NewMemoryStore(), rules.NewEngine())
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	body := bytes.NewBufferString(`{"seed":42}`)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/games", body)
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
	req = httptest.NewRequest(http.MethodPost, "/games/"+gameID+"/start", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("start status %d body %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/games/"+gameID+"/players/1/actions", nil)
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
	req := httptest.NewRequest(http.MethodPost, "/games", bytes.NewBufferString(`{"seed":88}`))
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
	req = httptest.NewRequest(http.MethodPost, "/games/"+gameID+"/start", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("start status %d body %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/games/"+gameID+"/players/1/trained-ai", nil)
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
	room := started["room"].(map[string]interface{})
	if room["status"] != "inProgress" {
		t.Fatalf("expected inProgress room, got %s", started)
	}
	ownGame := room["game"].(map[string]interface{})
	ownPlayers := ownGame["players"].(map[string]interface{})
	ownP1 := ownPlayers["1"].(map[string]interface{})
	if _, ok := ownP1["shares"].(map[string]interface{}); !ok {
		t.Fatalf("seated token should see own shares: %s", started)
	}

	observer := roomRequest(t, mux, http.MethodGet, "/room/state", "", nil)
	observerRoom := observer["room"].(map[string]interface{})
	observerGame := observerRoom["game"].(map[string]interface{})
	observerPlayers := observerGame["players"].(map[string]interface{})
	observerP1 := observerPlayers["1"].(map[string]interface{})
	if hidden, ok := observerP1["hiddenShareCount"].(float64); !ok || hidden == 0 {
		t.Fatalf("observer should see hidden share count for P1, got %s", observer)
	}
	shares := observerP1["shares"].(map[string]interface{})
	if len(shares) != 0 {
		t.Fatalf("observer should not see initial private shares, got %v", shares)
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
