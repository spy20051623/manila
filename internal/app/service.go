package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"manila/internal/ai"
	"manila/internal/model"
	"manila/internal/rules"
	"manila/internal/store"
)

const defaultTrainedGenomePath = "ai_weights_current/best_genome.json"
const defaultTrainedPoolPath = "ai_weights_current/genome_pool.json"

type Service struct {
	store         *store.MemoryStore
	engine        *rules.Engine
	aiMu          sync.Mutex
	aiAssignments map[string]map[int]ai.Genome
	roomMu        sync.Mutex
	room          roomState
	notify        func()
	notifyGame    func(string)
	timeoutTimer  *time.Timer
	closeTimer    *time.Timer
	offlineTimers map[string]*time.Timer
}

func NewService(st *store.MemoryStore, eng *rules.Engine) *Service {
	return &Service{
		store:         st,
		engine:        eng,
		aiAssignments: map[string]map[int]ai.Genome{},
		room:          newRoomState(),
		offlineTimers: map[string]*time.Timer{},
	}
}

func (s *Service) SetNotifier(fn func()) {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	s.notify = fn
}

func (s *Service) SetGameNotifier(fn func(string)) {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	s.notifyGame = fn
}

func (s *Service) CreateGame(seed *int64) *model.Game {
	actualSeed := time.Now().UnixNano()
	if seed != nil {
		actualSeed = *seed
	}
	id := s.store.NextID()
	g := rules.NewGame(id, actualSeed)
	s.store.Put(g)
	return g
}

func (s *Service) StartGame(id string) (*model.Game, error) {
	ref, ok := s.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	if err := s.engine.StartGame(ref.Game); err != nil {
		return nil, err
	}
	return ref.Game, nil
}

func (s *Service) GetGame(id string) (*model.Game, error) {
	ref, ok := s.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	return ref.Game, nil
}

func (s *Service) Events(id string) ([]model.Event, int, error) {
	ref, ok := s.store.Get(id)
	if !ok {
		return nil, 0, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	return append([]model.Event{}, ref.Game.Events...), ref.Game.EventSeq, nil
}

func (s *Service) LegalActions(id string, playerID int) ([]model.LegalAction, int, error) {
	ref, ok := s.store.Get(id)
	if !ok {
		return nil, 0, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	acts, err := s.engine.LegalActions(ref.Game, playerID)
	return acts, ref.Game.EventSeq, err
}

func (s *Service) ApplyAction(id string, action model.Action) (*model.Game, error) {
	ref, ok := s.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	action.GameID = id
	if err := s.engine.ApplyAction(ref.Game, action); err != nil {
		return nil, err
	}
	return ref.Game, nil
}

func (s *Service) ApplyRandomAI(id string, playerID int) (*model.Game, model.Action, error) {
	ref, ok := s.store.Get(id)
	if !ok {
		return nil, model.Action{}, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	if roomActorForPhase(ref.Game) != playerID {
		return nil, model.Action{}, fmt.Errorf("not player %d turn", playerID)
	}
	action, err := s.randomAction(ref.Game, playerID)
	if err != nil {
		return nil, model.Action{}, err
	}
	if err := s.engine.ApplyAction(ref.Game, action); err != nil {
		return nil, model.Action{}, err
	}
	return ref.Game, action, nil
}

func (s *Service) ApplyTrainedAI(id string, playerID int) (*model.Game, model.Action, error) {
	ref, ok := s.store.Get(id)
	if !ok {
		return nil, model.Action{}, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	if roomActorForPhase(ref.Game) != playerID {
		return nil, model.Action{}, fmt.Errorf("not player %d turn", playerID)
	}
	genome, err := s.trainedGenomeForPlayer(ref.Game, playerID)
	if err != nil {
		return nil, model.Action{}, err
	}
	agent := ai.NewEvolvableAgent(genome, ref.Game.Seed+int64(ref.Game.EventSeq+1)*7919+int64(playerID)*101)
	action, err := agent.ChooseAction(s.engine, ref.Game, playerID)
	if err != nil {
		return nil, model.Action{}, err
	}
	if err := s.engine.ApplyAction(ref.Game, action); err != nil {
		return nil, model.Action{}, err
	}
	return ref.Game, action, nil
}

func (s *Service) randomAction(g *model.Game, playerID int) (model.Action, error) {
	acts, err := s.engine.LegalActions(g, playerID)
	if err != nil {
		return model.Action{}, err
	}
	if len(acts) == 0 {
		return model.Action{}, fmt.Errorf("no legal actions")
	}
	seed := g.Seed + int64(g.EventSeq+1)*104729 + int64(playerID)*97
	r := rand.New(rand.NewSource(seed))
	chosen := acts[r.Intn(len(acts))]
	payload := copyPayload(chosen.Payload)
	switch chosen.Type {
	case model.ActionBid:
		minBid, _ := payloadInt(payload["minBid"])
		maxBid, _ := payloadInt(payload["maxBid"])
		if maxBid < minBid {
			return model.Action{}, fmt.Errorf("invalid bid range")
		}
		payload["amount"] = minBid + r.Intn(maxBid-minBid+1)
	case model.ActionSelectGoods:
		goods := append([]model.GoodsID{}, model.GoodsOrder...)
		r.Shuffle(len(goods), func(i, j int) { goods[i], goods[j] = goods[j], goods[i] })
		payload["goodsIds"] = []interface{}{int(goods[0]), int(goods[1]), int(goods[2])}
	case model.ActionSetShipStarts:
		options := []map[string]interface{}{
			{"1": 3, "2": 3, "3": 3, "4": 3},
			{"1": 5, "2": 4, "3": 0, "4": 0},
			{"1": 5, "2": 3, "3": 1, "4": 1},
			{"1": 4, "2": 4, "3": 1, "4": 1},
			{"1": 5, "2": 2, "3": 2, "4": 2},
		}
		starts := map[string]interface{}{}
		template := options[r.Intn(len(options))]
		sum := 0
		for _, gid := range g.Round.SelectedGoods {
			key := fmt.Sprint(int(gid))
			v, _ := payloadInt(template[key])
			starts[key] = v
			sum += v
		}
		if sum != 9 {
			starts = map[string]interface{}{}
			remaining := 9
			for i, gid := range g.Round.SelectedGoods {
				value := 3
				if i == len(g.Round.SelectedGoods)-1 {
					value = remaining
				}
				if value > 5 {
					value = 5
				}
				starts[fmt.Sprint(int(gid))] = value
				remaining -= value
			}
		}
		payload["starts"] = starts
	case model.ActionNavigatorMove:
		// Pure random AI keeps special movement simple and safe by submitting a no-op move.
		payload["moves"] = []interface{}{}
	}
	return model.Action{PlayerID: playerID, Type: chosen.Type, Payload: payload}, nil
}

func (s *Service) ResetGame(id string, seed *int64) (*model.Game, error) {
	ref, ok := s.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	actualSeed := ref.Game.Seed
	if seed != nil {
		actualSeed = *seed
	}
	ref.Game = rules.NewGame(id, actualSeed)
	s.clearAIAssignments(id)
	return ref.Game, nil
}

func (s *Service) SetSeed(id string, seed int64) (*model.Game, error) {
	ref, ok := s.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	if ref.Game.Status != model.StatusNotStarted {
		return nil, fmt.Errorf("seed can only be changed before game start")
	}
	ref.Game.Seed = seed
	return ref.Game, nil
}

func copyPayload(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func payloadInt(v interface{}) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case float64:
		return int(t), true
	default:
		return 0, false
	}
}

func loadDefaultTrainedGenome() (ai.Genome, error) {
	path, err := findUp(defaultTrainedGenomePath)
	if err != nil {
		return ai.Genome{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ai.Genome{}, fmt.Errorf("latest trained AI weights not found at %s: %w", defaultTrainedGenomePath, err)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var genome ai.Genome
	if err := json.Unmarshal(data, &genome); err != nil {
		return ai.Genome{}, fmt.Errorf("latest trained AI weights are invalid: %w", err)
	}
	if len(genome.Genes) == 0 {
		return ai.Genome{}, fmt.Errorf("latest trained AI weights have no genes")
	}
	return genome, nil
}

func (s *Service) trainedGenomeForPlayer(g *model.Game, playerID int) (ai.Genome, error) {
	s.aiMu.Lock()
	defer s.aiMu.Unlock()
	if assignments := s.aiAssignments[g.ID]; assignments != nil {
		if genome, ok := assignments[playerID]; ok {
			return genome, nil
		}
	}
	pool, err := loadDefaultTrainedPool()
	if err != nil {
		return ai.Genome{}, err
	}
	if len(pool) == 0 {
		return ai.Genome{}, fmt.Errorf("latest trained AI pool has no genomes")
	}
	assignments := s.aiAssignments[g.ID]
	if assignments == nil {
		assignments = assignGameAIPool(g, pool)
		s.aiAssignments[g.ID] = assignments
	}
	genome, ok := assignments[playerID]
	if ok {
		return genome, nil
	}
	seed := g.Seed + int64(playerID)*104729
	r := rand.New(rand.NewSource(seed))
	genome = pool[r.Intn(len(pool))]
	assignments[playerID] = genome
	return genome, nil
}

func assignGameAIPool(g *model.Game, pool []ai.Genome) map[int]ai.Genome {
	assignments := map[int]ai.Genome{}
	if len(pool) == 0 {
		return assignments
	}
	indexes := make([]int, len(pool))
	for i := range pool {
		indexes[i] = i
	}
	r := rand.New(rand.NewSource(g.Seed + 90917))
	r.Shuffle(len(indexes), func(i, j int) { indexes[i], indexes[j] = indexes[j], indexes[i] })
	for offset, playerID := range []int{2, 3, 4} {
		assignments[playerID] = pool[indexes[offset%len(indexes)]]
	}
	return assignments
}

func (s *Service) clearAIAssignments(gameID string) {
	s.aiMu.Lock()
	defer s.aiMu.Unlock()
	delete(s.aiAssignments, gameID)
}

func loadDefaultTrainedPool() ([]ai.Genome, error) {
	path, err := findUp(defaultTrainedPoolPath)
	if err == nil {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("latest trained AI pool not readable at %s: %w", defaultTrainedPoolPath, readErr)
		}
		data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
		var pool struct {
			Entries []struct {
				Genome ai.Genome `json:"genome"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(data, &pool); err != nil {
			return nil, fmt.Errorf("latest trained AI pool is invalid: %w", err)
		}
		genomes := make([]ai.Genome, 0, len(pool.Entries))
		for _, entry := range pool.Entries {
			if len(entry.Genome.Genes) > 0 {
				genomes = append(genomes, entry.Genome)
			}
		}
		if len(genomes) > 0 {
			return genomes, nil
		}
	}
	genome, err := loadDefaultTrainedGenome()
	if err != nil {
		return nil, err
	}
	return []ai.Genome{genome}, nil
}

func findUp(relativePath string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, relativePath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("latest trained AI weights not found at %s", relativePath)
		}
		dir = parent
	}
}
