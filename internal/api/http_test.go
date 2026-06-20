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
