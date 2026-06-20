package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"manila/internal/api"
	"manila/internal/app"
	"manila/internal/rules"
	"manila/internal/store"
)

func main() {
	st := store.NewMemoryStore()
	engine := rules.NewEngine()
	service := app.NewService(st, engine)
	handler := api.NewHandler(service)

	mux := http.NewServeMux()
	handler.Register(mux)

	addr := loadListenAddr()
	log.Println("manila backend listening on " + addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func loadListenAddr() string {
	cfg := struct {
		ListenAddr string `json:"listenAddr"`
	}{
		ListenAddr: "localhost:8080",
	}
	if data, err := os.ReadFile("config.json"); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			log.Fatalf("invalid config.json: %v", err)
		}
	}
	if env := os.Getenv("MANILA_ADDR"); env != "" {
		return env
	}
	if cfg.ListenAddr == "" {
		return "localhost:8080"
	}
	return cfg.ListenAddr
}
