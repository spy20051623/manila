package training

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"manila/internal/ai"
	"manila/internal/model"
	"manila/internal/rules"
)

type RandomGameSummary struct {
	Seed        int64         `json:"seed"`
	Steps       int           `json:"steps"`
	Ended       bool          `json:"ended"`
	FinalScores []model.Score `json:"finalScores,omitempty"`
	Error       string        `json:"error,omitempty"`
}

type RandomBatchResult struct {
	Games []RandomGameSummary `json:"games"`
}

type EvaluationConfig struct {
	SeedsPerSeat   int         `json:"seedsPerSeat"`
	MaxSteps       int         `json:"maxSteps"`
	BaseSeed       int64       `json:"baseSeed"`
	OpponentMode   string      `json:"opponentMode,omitempty"`
	BaselineGenome *ai.Genome  `json:"baselineGenome,omitempty"`
	BaselinePool   []ai.Genome `json:"baselinePool,omitempty"`
}

type EvaluationResult struct {
	Genome       ai.Genome           `json:"genome"`
	Weights      ai.Weights          `json:"weights"`
	AverageScore float64             `json:"averageScore"`
	Games        int                 `json:"games"`
	Wins         int                 `json:"wins"`
	Behavior     BehaviorStats       `json:"behavior"`
	Errors       []string            `json:"errors,omitempty"`
	SeatScores   map[int]float64     `json:"seatScores"`
	Summaries    []RandomGameSummary `json:"summaries,omitempty"`
}

type BehaviorStats struct {
	Bids                float64 `json:"bids"`
	BidAmount           float64 `json:"bidAmount"`
	SharesBought        float64 `json:"sharesBought"`
	ShipPlacements      float64 `json:"shipPlacements"`
	PortPlacements      float64 `json:"portPlacements"`
	DockPlacements      float64 `json:"dockPlacements"`
	PiratePlacements    float64 `json:"piratePlacements"`
	NavigatorPlacements float64 `json:"navigatorPlacements"`
	InsurancePlacements float64 `json:"insurancePlacements"`
	PirateBoards        float64 `json:"pirateBoards"`
	PirateLootsToPort   float64 `json:"pirateLootsToPort"`
	NavigatorMoves      float64 `json:"navigatorMoves"`
	Bankruptcies        float64 `json:"bankruptcies"`
}

type TrainConfig struct {
	Population                    int         `json:"population"`
	Generations                   int         `json:"generations"`
	SeedsPerSeat                  int         `json:"seedsPerSeat"`
	MaxSteps                      int         `json:"maxSteps"`
	BaseSeed                      int64       `json:"baseSeed"`
	MutationSigma                 float64     `json:"mutationSigma"`
	MutationStart                 float64     `json:"mutationStart,omitempty"`
	MutationEnd                   float64     `json:"mutationEnd,omitempty"`
	MutationFocus                 []string    `json:"mutationFocus,omitempty"`
	MutationResetProbability      float64     `json:"mutationResetProbability,omitempty"`
	MutationFocusResetProbability float64     `json:"mutationFocusResetProbability,omitempty"`
	MutationFocusSigmaMultiplier  float64     `json:"mutationFocusSigmaMultiplier,omitempty"`
	Workers                       int         `json:"workers,omitempty"`
	EliteCount                    int         `json:"eliteCount,omitempty"`
	MinEliteDiversity             float64     `json:"minEliteDiversity,omitempty"`
	PoolSize                      int         `json:"poolSize,omitempty"`
	MinPoolDiversity              float64     `json:"minPoolDiversity,omitempty"`
	OutputDir                     string      `json:"outputDir"`
	OpponentMode                  string      `json:"opponentMode,omitempty"`
	BaselineGenome                *ai.Genome  `json:"baselineGenome,omitempty"`
	BaselinePool                  []ai.Genome `json:"baselinePool,omitempty"`
	ProgressWriter                io.Writer   `json:"-"`
}

type GenerationSummary struct {
	Generation   int        `json:"generation"`
	BestScore    float64    `json:"bestScore"`
	AverageScore float64    `json:"averageScore"`
	BestGenome   ai.Genome  `json:"bestGenome"`
	BestWeights  ai.Weights `json:"bestWeights"`
}

type TrainResult struct {
	Config      TrainConfig         `json:"config"`
	BestScore   float64             `json:"bestScore"`
	BestGenome  ai.Genome           `json:"bestGenome"`
	BestWeights ai.Weights          `json:"bestWeights"`
	Pool        GenomePool          `json:"pool"`
	History     []GenerationSummary `json:"history"`
	OutputDir   string              `json:"outputDir,omitempty"`
}

type GenomePool struct {
	Version int               `json:"version"`
	Entries []GenomePoolEntry `json:"entries"`
}

type GenomePoolEntry struct {
	ID      string     `json:"id"`
	Score   float64    `json:"score"`
	Genome  ai.Genome  `json:"genome"`
	Weights ai.Weights `json:"weights"`
}

type PoolEvaluationResult struct {
	Mode                   string             `json:"mode"`
	CandidateCount         int                `json:"candidateCount"`
	BaselineCount          int                `json:"baselineCount"`
	ForwardAverageScore    float64            `json:"forwardAverageScore,omitempty"`
	ReverseOldSoloScore    float64            `json:"reverseOldSoloScore,omitempty"`
	CombinedAdvantage      float64            `json:"combinedAdvantage,omitempty"`
	ForwardResults         []EvaluationResult `json:"forwardResults,omitempty"`
	ReverseBaselineResults []EvaluationResult `json:"reverseBaselineResults,omitempty"`
}

type PoolMergeConfig struct {
	PoolSize       int     `json:"poolSize"`
	MinDiversity   float64 `json:"minDiversity"`
	BehaviorWeight float64 `json:"behaviorWeight"`
}

type poolMergeCandidate struct {
	id       string
	source   string
	index    int
	score    float64
	genome   ai.Genome
	weights  ai.Weights
	behavior BehaviorStats
}

type scoredGenome struct {
	genome ai.Genome
	eval   EvaluationResult
}

func RunRandomGames(count int, seed int64, maxSteps int) RandomBatchResult {
	if count < 1 {
		count = 1
	}
	if maxSteps < 1 {
		maxSteps = 500
	}
	engine := rules.NewEngine()
	result := RandomBatchResult{Games: make([]RandomGameSummary, 0, count)}
	for i := 0; i < count; i++ {
		gameSeed := seed + int64(i)
		g := rules.NewGame(fmt.Sprintf("training-%d", i+1), gameSeed)
		summary := RandomGameSummary{Seed: gameSeed}
		if err := engine.StartGame(g); err != nil {
			summary.Error = err.Error()
			result.Games = append(result.Games, summary)
			continue
		}
		agents := map[int]ai.Agent{}
		for _, pid := range model.PlayerOrder {
			agents[pid] = ai.NewRandomAgent(gameSeed + int64(pid)*1009)
		}
		runGame(engine, g, agents, maxSteps, &summary)
		result.Games = append(result.Games, summary)
	}
	return result
}

func RandomAction(engine *rules.Engine, g *model.Game, playerID int) (model.Action, error) {
	return ai.NewRandomAgent(g.Seed+int64(g.EventSeq+1)*104729+int64(playerID)*97).ChooseAction(engine, g, playerID)
}

func EvaluateGenome(genome ai.Genome, cfg EvaluationConfig) EvaluationResult {
	cfg = normalizeEvaluationConfig(cfg)
	engine := rules.NewEngine()
	result := EvaluationResult{
		Genome:     genome,
		Weights:    ai.DecodeGenome(genome),
		SeatScores: map[int]float64{},
	}
	for _, candidateSeat := range model.PlayerOrder {
		seatTotal := 0.0
		for i := 0; i < cfg.SeedsPerSeat; i++ {
			gameSeed := cfg.BaseSeed + int64(candidateSeat)*100000 + int64(i)
			g := rules.NewGame(fmt.Sprintf("eval-seat%d-%d", candidateSeat, i+1), gameSeed)
			summary := RandomGameSummary{Seed: gameSeed}
			if err := engine.StartGame(g); err != nil {
				summary.Error = err.Error()
				result.Errors = append(result.Errors, err.Error())
				result.Summaries = append(result.Summaries, summary)
				continue
			}
			agents := agentsForEvaluation(genome, cfg, candidateSeat, gameSeed)
			runGame(engine, g, agents, cfg.MaxSteps, &summary)
			result.Behavior.add(behaviorStatsForPlayer(g, candidateSeat))
			scores := scoresForGame(g)
			rewards := rewardsFromScores(scores)
			reward := rewards[candidateSeat]
			seatTotal += reward
			result.AverageScore += reward
			result.Games++
			if isWinner(scores, candidateSeat) {
				result.Wins++
			}
			if summary.Error != "" {
				result.Errors = append(result.Errors, summary.Error)
			}
			result.Summaries = append(result.Summaries, summary)
		}
		result.SeatScores[candidateSeat] = seatTotal / float64(cfg.SeedsPerSeat)
	}
	if result.Games > 0 {
		result.AverageScore /= float64(result.Games)
		result.Behavior.scale(1 / float64(result.Games))
	}
	return result
}

func TrainPopulation(cfg TrainConfig) (TrainResult, error) {
	cfg = normalizeTrainConfig(cfg)
	focusGenes, err := mutationFocusGenesFromConfig(cfg)
	if err != nil {
		return TrainResult{}, err
	}
	r := rand.New(rand.NewSource(cfg.BaseSeed))
	population := make([]ai.Genome, cfg.Population)
	population[0] = ai.NeutralGenome()
	start := 1
	if cfg.BaselineGenome != nil && cfg.Population > 1 {
		population[1] = *cfg.BaselineGenome
		start = 2
	}
	for i := start; i < cfg.Population; i++ {
		population[i] = ai.RandomGenome(r)
	}
	result := TrainResult{
		Config:    cfg,
		BestScore: math.Inf(-1),
		OutputDir: cfg.OutputDir,
	}
	elites := cfg.EliteCount
	for generation := 1; generation <= cfg.Generations; generation++ {
		scored := evaluatePopulation(population, cfg, generation)
		total := 0.0
		for _, item := range scored {
			total += item.eval.AverageScore
		}
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].eval.AverageScore > scored[j].eval.AverageScore
		})
		best := scored[0].eval
		if best.AverageScore > result.BestScore {
			result.BestScore = best.AverageScore
			result.BestGenome = scored[0].genome
			result.BestWeights = best.Weights
		}
		result.History = append(result.History, GenerationSummary{
			Generation:   generation,
			BestScore:    best.AverageScore,
			AverageScore: total / float64(len(scored)),
			BestGenome:   scored[0].genome,
			BestWeights:  best.Weights,
		})
		if cfg.ProgressWriter != nil {
			fmt.Fprintf(cfg.ProgressWriter, "generation %d: best=%.4f average=%.4f\n", generation, best.AverageScore, total/float64(len(scored)))
		}
		if generation == cfg.Generations {
			result.Pool = buildGenomePool(scored, cfg.PoolSize, cfg.MinPoolDiversity)
			break
		}
		elitePool := selectDiverseElites(scored, elites, cfg.MinEliteDiversity)
		next := make([]ai.Genome, 0, cfg.Population)
		for _, elite := range elitePool {
			next = append(next, elite.genome)
		}
		sigma := mutationSigmaForGeneration(cfg, generation)
		mutationProfile := mutationProfileForGeneration(cfg, focusGenes, generation)
		for len(next) < cfg.Population {
			parent := elitePool[r.Intn(len(elitePool))].genome
			next = append(next, ai.MutateGenomeWithProfile(parent, r, sigma, mutationProfile))
		}
		population = next
	}
	if cfg.OutputDir != "" {
		if err := WriteTrainingArtifacts(cfg.OutputDir, result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func evaluatePopulation(population []ai.Genome, cfg TrainConfig, generation int) []scoredGenome {
	scored := make([]scoredGenome, len(population))
	jobs := make(chan int)
	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(population) {
		workers = len(population)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				genome := population[i]
				evalSeed := cfg.BaseSeed + int64(generation)*10000000 + int64(i)*100000
				eval := EvaluateGenome(genome, EvaluationConfig{
					SeedsPerSeat:   cfg.SeedsPerSeat,
					MaxSteps:       cfg.MaxSteps,
					BaseSeed:       evalSeed,
					OpponentMode:   cfg.OpponentMode,
					BaselineGenome: cfg.BaselineGenome,
					BaselinePool:   cfg.BaselinePool,
				})
				scored[i] = scoredGenome{genome: genome, eval: eval}
			}
		}()
	}
	for i := range population {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return scored
}

func selectDiverseElites(scored []scoredGenome, count int, minDistance float64) []scoredGenome {
	if count < 1 {
		count = 1
	}
	if count > len(scored) {
		count = len(scored)
	}
	if len(scored) == 0 {
		return nil
	}
	selected := []scoredGenome{scored[0]}
	threshold := minDistance
	for len(selected) < count && threshold > 0.0001 {
		for _, candidate := range scored[1:] {
			if len(selected) >= count {
				break
			}
			if containsGenome(selected, candidate.genome) {
				continue
			}
			if minGenomeDistance(candidate.genome, selected) >= threshold {
				selected = append(selected, candidate)
			}
		}
		threshold *= 0.75
	}
	for _, candidate := range scored[1:] {
		if len(selected) >= count {
			break
		}
		if !containsGenome(selected, candidate.genome) {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func buildGenomePool(scored []scoredGenome, count int, minDistance float64) GenomePool {
	selected := selectDiverseElites(scored, count, minDistance)
	pool := GenomePool{Version: 1, Entries: make([]GenomePoolEntry, 0, len(selected))}
	for i, item := range selected {
		pool.Entries = append(pool.Entries, GenomePoolEntry{
			ID:      fmt.Sprintf("ai-%02d", i+1),
			Score:   item.eval.AverageScore,
			Genome:  item.genome,
			Weights: item.eval.Weights,
		})
	}
	return pool
}

func minGenomeDistance(genome ai.Genome, selected []scoredGenome) float64 {
	minDistance := math.Inf(1)
	for _, item := range selected {
		distance := genomeDistance(genome, item.genome)
		if distance < minDistance {
			minDistance = distance
		}
	}
	return minDistance
}

func genomeDistance(a, b ai.Genome) float64 {
	sum := 0.0
	for i := 0; i < ai.GeneCount; i++ {
		av := 0.5
		bv := 0.5
		if i < len(a.Genes) {
			av = a.Genes[i]
		}
		if i < len(b.Genes) {
			bv = b.Genes[i]
		}
		diff := av - bv
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(ai.GeneCount))
}

func containsGenome(items []scoredGenome, genome ai.Genome) bool {
	for _, item := range items {
		if genomeDistance(item.genome, genome) < 0.0000001 {
			return true
		}
	}
	return false
}

func mutationSigmaForGeneration(cfg TrainConfig, generation int) float64 {
	if cfg.Generations <= 1 {
		return cfg.MutationEnd
	}
	progress := float64(generation-1) / float64(cfg.Generations-1)
	return cfg.MutationStart * math.Pow(cfg.MutationEnd/cfg.MutationStart, progress)
}

func mutationFocusGenesFromConfig(cfg TrainConfig) (map[int]bool, error) {
	focusGenes := map[int]bool{}
	for _, raw := range cfg.MutationFocus {
		for _, token := range strings.Split(raw, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			if index, err := strconv.Atoi(token); err == nil {
				if index < 0 || index >= ai.GeneCount {
					return nil, fmt.Errorf("mutation focus index %d outside range 0-%d", index, ai.GeneCount-1)
				}
				focusGenes[index] = true
				continue
			}
			index, ok := ai.GeneIndex(token)
			if !ok {
				return nil, fmt.Errorf("unknown mutation focus gene %q", token)
			}
			focusGenes[index] = true
		}
	}
	return focusGenes, nil
}

func mutationProfileForGeneration(cfg TrainConfig, focusGenes map[int]bool, generation int) ai.MutationProfile {
	decay := mutationFocusDecay(cfg, generation)
	focusResetProbability := cfg.MutationResetProbability +
		(cfg.MutationFocusResetProbability-cfg.MutationResetProbability)*decay
	focusSigmaMultiplier := 1 + (cfg.MutationFocusSigmaMultiplier-1)*decay
	return ai.MutationProfile{
		FocusGenes:            focusGenes,
		BaseResetProbability:  cfg.MutationResetProbability,
		FocusResetProbability: focusResetProbability,
		FocusSigmaMultiplier:  focusSigmaMultiplier,
	}
}

func mutationFocusDecay(cfg TrainConfig, generation int) float64 {
	if len(cfg.MutationFocus) == 0 || cfg.Generations <= 2 {
		return 0
	}
	progress := float64(generation-1) / float64(cfg.Generations-2)
	return clampFloat(1-progress, 0, 1)
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func WriteTrainingArtifacts(dir string, result TrainResult) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	files := map[string]interface{}{
		"best_genome.json":     result.BestGenome,
		"best_weights.json":    result.BestWeights,
		"genome_pool.json":     result.Pool,
		"training_result.json": result,
	}
	for name, value := range files {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0644); err != nil {
			return err
		}
	}
	return nil
}

func WriteGenomePoolArtifacts(dir string, pool GenomePool) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(dir, "genome_pool.json"), pool); err != nil {
		return err
	}
	if len(pool.Entries) > 0 {
		if err := writeJSONFile(filepath.Join(dir, "best_genome.json"), pool.Entries[0].Genome); err != nil {
			return err
		}
		if err := writeJSONFile(filepath.Join(dir, "best_weights.json"), pool.Entries[0].Weights); err != nil {
			return err
		}
	}
	return nil
}

func writeJSONFile(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func LoadGenome(path string) (ai.Genome, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ai.Genome{}, err
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var genome ai.Genome
	if err := json.Unmarshal(data, &genome); err != nil {
		return ai.Genome{}, err
	}
	if len(genome.Genes) == 0 {
		return ai.Genome{}, fmt.Errorf("genome file has no genes")
	}
	return genome, nil
}

func LoadGenomePool(path string) ([]ai.Genome, error) {
	resolved := path
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		resolved = filepath.Join(path, "genome_pool.json")
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			return loadGenomePoolFallback(path)
		}
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var pool GenomePool
	if err := json.Unmarshal(data, &pool); err == nil && len(pool.Entries) > 0 {
		genomes := make([]ai.Genome, 0, len(pool.Entries))
		for _, entry := range pool.Entries {
			if len(entry.Genome.Genes) == 0 {
				continue
			}
			genomes = append(genomes, entry.Genome)
		}
		if len(genomes) > 0 {
			return genomes, nil
		}
	}
	var genome ai.Genome
	if err := json.Unmarshal(data, &genome); err != nil {
		return nil, err
	}
	if len(genome.Genes) == 0 {
		return nil, fmt.Errorf("genome pool has no genomes")
	}
	return []ai.Genome{genome}, nil
}

func loadGenomePoolFallback(dir string) ([]ai.Genome, error) {
	genome, err := LoadGenome(filepath.Join(dir, "best_genome.json"))
	if err != nil {
		return nil, err
	}
	return []ai.Genome{genome}, nil
}

func agentsForEvaluation(genome ai.Genome, cfg EvaluationConfig, candidateSeat int, gameSeed int64) map[int]ai.Agent {
	agents := map[int]ai.Agent{}
	opponentSeats := []int{}
	for _, pid := range model.PlayerOrder {
		if pid == candidateSeat {
			agents[pid] = ai.NewEvolvableAgent(genome, gameSeed+int64(pid)*7919)
		} else {
			opponentSeats = append(opponentSeats, pid)
		}
	}
	if cfg.OpponentMode == "blended" {
		r := rand.New(rand.NewSource(gameSeed + int64(candidateSeat)*6173))
		for i, pid := range opponentSeats {
			switch i {
			case 0:
				agents[pid] = ai.NewRandomAgent(gameSeed + int64(pid)*1009)
			case 1:
				agents[pid] = ai.NewGreedyAgent(gameSeed + int64(pid)*2003)
			default:
				if len(cfg.BaselinePool) > 0 {
					opponent := cfg.BaselinePool[r.Intn(len(cfg.BaselinePool))]
					agents[pid] = ai.NewEvolvableAgent(opponent, gameSeed+int64(pid)*3001)
				} else if cfg.BaselineGenome != nil {
					agents[pid] = ai.NewEvolvableAgent(*cfg.BaselineGenome, gameSeed+int64(pid)*3001)
				} else {
					agents[pid] = ai.NewGreedyAgent(gameSeed + int64(pid)*3001)
				}
			}
		}
		return agents
	}
	if cfg.OpponentMode == "pool" && len(cfg.BaselinePool) > 0 {
		r := rand.New(rand.NewSource(gameSeed + int64(candidateSeat)*4241))
		for _, pid := range opponentSeats {
			opponent := cfg.BaselinePool[r.Intn(len(cfg.BaselinePool))]
			agents[pid] = ai.NewEvolvableAgent(opponent, gameSeed+int64(pid)*3001)
		}
		return agents
	}
	if cfg.OpponentMode != "mixed" {
		for _, pid := range opponentSeats {
			agents[pid] = ai.NewRandomAgent(gameSeed + int64(pid)*1009)
		}
		return agents
	}
	for i, pid := range opponentSeats {
		switch i {
		case 0:
			agents[pid] = ai.NewRandomAgent(gameSeed + int64(pid)*1009)
		case 1:
			agents[pid] = ai.NewGreedyAgent(gameSeed + int64(pid)*2003)
		default:
			if cfg.BaselineGenome != nil {
				agents[pid] = ai.NewEvolvableAgent(*cfg.BaselineGenome, gameSeed+int64(pid)*3001)
			} else {
				agents[pid] = ai.NewGreedyAgent(gameSeed + int64(pid)*3001)
			}
		}
	}
	return agents
}

func EvaluateGenomePool(candidates []ai.Genome, baselines []ai.Genome, cfg EvaluationConfig, mode string) PoolEvaluationResult {
	if mode == "" {
		mode = "both"
	}
	result := PoolEvaluationResult{
		Mode:           mode,
		CandidateCount: len(candidates),
		BaselineCount:  len(baselines),
	}
	if (mode == "forward" || mode == "both") && len(candidates) > 0 {
		total := 0.0
		for i, genome := range candidates {
			evalCfg := cfg
			evalCfg.OpponentMode = "pool"
			evalCfg.BaselinePool = baselines
			evalCfg.BaseSeed = cfg.BaseSeed + int64(i)*1000000
			eval := EvaluateGenome(genome, evalCfg)
			result.ForwardResults = append(result.ForwardResults, eval)
			total += eval.AverageScore
		}
		result.ForwardAverageScore = total / float64(len(candidates))
	}
	if (mode == "reverse" || mode == "both") && len(baselines) > 0 {
		total := 0.0
		for i, genome := range baselines {
			evalCfg := cfg
			evalCfg.OpponentMode = "pool"
			evalCfg.BaselinePool = candidates
			evalCfg.BaseSeed = cfg.BaseSeed + 500000000 + int64(i)*1000000
			eval := EvaluateGenome(genome, evalCfg)
			result.ReverseBaselineResults = append(result.ReverseBaselineResults, eval)
			total += eval.AverageScore
		}
		result.ReverseOldSoloScore = total / float64(len(baselines))
	}
	if len(result.ForwardResults) > 0 || len(result.ReverseBaselineResults) > 0 {
		result.CombinedAdvantage = result.ForwardAverageScore - result.ReverseOldSoloScore
	}
	return result
}

func MergeEvaluatedPools(result PoolEvaluationResult, cfg PoolMergeConfig) GenomePool {
	if cfg.PoolSize < 1 {
		cfg.PoolSize = 8
	}
	if cfg.MinDiversity <= 0 {
		cfg.MinDiversity = 0.20
	}
	if cfg.BehaviorWeight < 0 {
		cfg.BehaviorWeight = 0
	}
	candidates := poolMergeCandidates(result)
	selected := selectDiversePoolCandidates(candidates, cfg.PoolSize, cfg.MinDiversity, cfg.BehaviorWeight)
	pool := GenomePool{Version: 1, Entries: make([]GenomePoolEntry, 0, len(selected))}
	for i, item := range selected {
		pool.Entries = append(pool.Entries, GenomePoolEntry{
			ID:      fmt.Sprintf("%s-%02d-rank-%02d", item.source, item.index, i+1),
			Score:   item.score,
			Genome:  item.genome,
			Weights: item.weights,
		})
	}
	return pool
}

func poolMergeCandidates(result PoolEvaluationResult) []poolMergeCandidate {
	candidates := []poolMergeCandidate{}
	for i, eval := range result.ForwardResults {
		candidates = append(candidates, poolMergeCandidate{
			id:       fmt.Sprintf("new-%02d", i+1),
			source:   "new",
			index:    i + 1,
			score:    eval.AverageScore,
			genome:   eval.Genome,
			weights:  eval.Weights,
			behavior: eval.Behavior,
		})
	}
	for i, eval := range result.ReverseBaselineResults {
		candidates = append(candidates, poolMergeCandidate{
			id:       fmt.Sprintf("old-%02d", i+1),
			source:   "old",
			index:    i + 1,
			score:    eval.AverageScore,
			genome:   eval.Genome,
			weights:  eval.Weights,
			behavior: eval.Behavior,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].id < candidates[j].id
		}
		return candidates[i].score > candidates[j].score
	})
	return candidates
}

func selectDiversePoolCandidates(candidates []poolMergeCandidate, count int, minDistance float64, behaviorWeight float64) []poolMergeCandidate {
	if count > len(candidates) {
		count = len(candidates)
	}
	selected := []poolMergeCandidate{}
	threshold := minDistance
	for len(selected) < count && threshold > 0.0001 {
		for _, candidate := range candidates {
			if len(selected) >= count {
				break
			}
			if containsPoolCandidateGenome(selected, candidate.genome) {
				continue
			}
			if minPoolCandidateDistance(candidate, selected, behaviorWeight) >= threshold {
				selected = append(selected, candidate)
			}
		}
		threshold *= 0.75
	}
	for _, candidate := range candidates {
		if len(selected) >= count {
			break
		}
		if !containsPoolCandidateGenome(selected, candidate.genome) {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func minPoolCandidateDistance(candidate poolMergeCandidate, selected []poolMergeCandidate, behaviorWeight float64) float64 {
	if len(selected) == 0 {
		return math.Inf(1)
	}
	minDistance := math.Inf(1)
	for _, item := range selected {
		d := genomeDistance(candidate.genome, item.genome) + behaviorWeight*behaviorDistance(candidate.behavior, item.behavior)
		if d < minDistance {
			minDistance = d
		}
	}
	return minDistance
}

func containsPoolCandidateGenome(items []poolMergeCandidate, genome ai.Genome) bool {
	for _, item := range items {
		if genomeDistance(item.genome, genome) < 0.0000001 {
			return true
		}
	}
	return false
}

func runGame(engine *rules.Engine, g *model.Game, agents map[int]ai.Agent, maxSteps int, summary *RandomGameSummary) {
	if maxSteps < 1 {
		maxSteps = 500
	}
	for step := 0; step < maxSteps && g.Status != model.StatusEnded; step++ {
		agent := agents[g.CurrentPlayer]
		if agent == nil {
			summary.Error = fmt.Sprintf("no agent for player %d", g.CurrentPlayer)
			break
		}
		action, err := agent.ChooseAction(engine, g, g.CurrentPlayer)
		if err != nil {
			summary.Error = err.Error()
			break
		}
		if err := engine.ApplyAction(g, action); err != nil {
			summary.Error = err.Error()
			break
		}
		summary.Steps = step + 1
	}
	summary.Ended = g.Status == model.StatusEnded
	summary.FinalScores = scoresForGame(g)
}

func behaviorStatsForPlayer(g *model.Game, playerID int) BehaviorStats {
	stats := BehaviorStats{}
	for _, event := range g.Events {
		if event.Data == nil {
			continue
		}
		eventPlayer := intFromEventData(event.Data["playerId"])
		if eventPlayer != playerID {
			continue
		}
		switch event.Type {
		case "BidPlaced":
			stats.Bids++
			stats.BidAmount += float64(intFromEventData(event.Data["amount"]))
		case "ShareBought":
			stats.SharesBought++
		case "AccomplicePlaced":
			switch fmt.Sprint(event.Data["positionType"]) {
			case "ship":
				stats.ShipPlacements++
			case "port":
				stats.PortPlacements++
			case "dock":
				stats.DockPlacements++
			case "pirate":
				stats.PiratePlacements++
			case "navigatorSmall", "navigatorBig":
				stats.NavigatorPlacements++
			case "insurance":
				stats.InsurancePlacements++
			}
		case "PirateBoarded":
			stats.PirateBoards++
		case "PiratesLootedShip":
			if fmt.Sprint(event.Data["destination"]) == "port" {
				stats.PirateLootsToPort++
			}
		case "NavigatorMoved":
			stats.NavigatorMoves++
		case "PlayerBecameBankrupt":
			stats.Bankruptcies++
		}
	}
	return stats
}

func (s *BehaviorStats) add(other BehaviorStats) {
	s.Bids += other.Bids
	s.BidAmount += other.BidAmount
	s.SharesBought += other.SharesBought
	s.ShipPlacements += other.ShipPlacements
	s.PortPlacements += other.PortPlacements
	s.DockPlacements += other.DockPlacements
	s.PiratePlacements += other.PiratePlacements
	s.NavigatorPlacements += other.NavigatorPlacements
	s.InsurancePlacements += other.InsurancePlacements
	s.PirateBoards += other.PirateBoards
	s.PirateLootsToPort += other.PirateLootsToPort
	s.NavigatorMoves += other.NavigatorMoves
	s.Bankruptcies += other.Bankruptcies
}

func (s *BehaviorStats) scale(factor float64) {
	s.Bids *= factor
	s.BidAmount *= factor
	s.SharesBought *= factor
	s.ShipPlacements *= factor
	s.PortPlacements *= factor
	s.DockPlacements *= factor
	s.PiratePlacements *= factor
	s.NavigatorPlacements *= factor
	s.InsurancePlacements *= factor
	s.PirateBoards *= factor
	s.PirateLootsToPort *= factor
	s.NavigatorMoves *= factor
	s.Bankruptcies *= factor
}

func intFromEventData(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func behaviorDistance(a, b BehaviorStats) float64 {
	av := behaviorVector(a)
	bv := behaviorVector(b)
	sum := 0.0
	for i := range av {
		diff := av[i] - bv[i]
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(len(av)))
}

func behaviorVector(s BehaviorStats) []float64 {
	return []float64{
		s.Bids / 12,
		s.BidAmount / 80,
		s.SharesBought / 8,
		s.ShipPlacements / 9,
		s.PortPlacements / 5,
		s.DockPlacements / 5,
		s.PiratePlacements / 4,
		s.NavigatorPlacements / 4,
		s.InsurancePlacements / 4,
		s.PirateBoards / 3,
		s.PirateLootsToPort / 3,
		s.NavigatorMoves / 4,
		s.Bankruptcies / 3,
	}
}

func scoresForGame(g *model.Game) []model.Score {
	if len(g.FinalScores) > 0 {
		out := append([]model.Score{}, g.FinalScores...)
		return out
	}
	scores := []model.Score{}
	for _, pid := range model.PlayerOrder {
		p := g.Players[pid]
		wealth := p.Cash
		for _, gid := range model.GoodsOrder {
			wealth += p.Shares[gid] * g.Goods[gid].MarketValue()
		}
		wealth -= p.MortgagedShareCount * 15
		scores = append(scores, model.Score{PlayerID: pid, Wealth: wealth})
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Wealth == scores[j].Wealth {
			return scores[i].PlayerID < scores[j].PlayerID
		}
		return scores[i].Wealth > scores[j].Wealth
	})
	rank := 1
	for i := range scores {
		if i > 0 && scores[i].Wealth < scores[i-1].Wealth {
			rank = i + 1
		}
		scores[i].Rank = rank
	}
	return scores
}

func rewardsFromScores(scores []model.Score) map[int]float64 {
	base := []float64{1, 0.3, -0.3, -1}
	rewards := map[int]float64{}
	for i := 0; i < len(scores); {
		j := i + 1
		for j < len(scores) && scores[j].Wealth == scores[i].Wealth {
			j++
		}
		sum := 0.0
		for pos := i; pos < j && pos < len(base); pos++ {
			sum += base[pos]
		}
		avg := sum / float64(j-i)
		for k := i; k < j; k++ {
			rewards[scores[k].PlayerID] = avg
		}
		i = j
	}
	return rewards
}

func isWinner(scores []model.Score, playerID int) bool {
	if len(scores) == 0 {
		return false
	}
	top := scores[0].Wealth
	for _, score := range scores {
		if score.PlayerID == playerID {
			return score.Wealth == top
		}
	}
	return false
}

func normalizeEvaluationConfig(cfg EvaluationConfig) EvaluationConfig {
	if cfg.SeedsPerSeat < 1 {
		cfg.SeedsPerSeat = 8
	}
	if cfg.MaxSteps < 1 {
		cfg.MaxSteps = 600
	}
	if cfg.BaseSeed == 0 {
		cfg.BaseSeed = 1
	}
	if cfg.OpponentMode == "" {
		cfg.OpponentMode = "random"
	}
	return cfg
}

func normalizeTrainConfig(cfg TrainConfig) TrainConfig {
	if cfg.Population < 1 {
		cfg.Population = 24
	}
	if cfg.Generations < 1 {
		cfg.Generations = 8
	}
	if cfg.SeedsPerSeat < 1 {
		cfg.SeedsPerSeat = 8
	}
	if cfg.MaxSteps < 1 {
		cfg.MaxSteps = 600
	}
	if cfg.BaseSeed == 0 {
		cfg.BaseSeed = 1
	}
	if cfg.MutationSigma <= 0 {
		cfg.MutationSigma = 0.12
	}
	if cfg.MutationStart <= 0 {
		cfg.MutationStart = cfg.MutationSigma
	}
	if cfg.MutationEnd <= 0 {
		cfg.MutationEnd = cfg.MutationSigma
	}
	if cfg.MutationResetProbability <= 0 {
		cfg.MutationResetProbability = 0.12
	}
	if cfg.MutationFocusResetProbability <= 0 {
		cfg.MutationFocusResetProbability = 0.28
	}
	if cfg.MutationFocusSigmaMultiplier <= 0 {
		cfg.MutationFocusSigmaMultiplier = 2.0
	}
	if cfg.Workers < 1 {
		cfg.Workers = runtime.NumCPU()
	}
	if cfg.EliteCount < 1 {
		cfg.EliteCount = cfg.Population / 3
	}
	if cfg.EliteCount < 4 && cfg.Population >= 4 {
		cfg.EliteCount = 4
	}
	if cfg.EliteCount > cfg.Population {
		cfg.EliteCount = cfg.Population
	}
	if cfg.MinEliteDiversity <= 0 {
		cfg.MinEliteDiversity = 0.18
	}
	if cfg.PoolSize < 1 {
		cfg.PoolSize = cfg.EliteCount
	}
	if cfg.PoolSize > cfg.Population {
		cfg.PoolSize = cfg.Population
	}
	if cfg.MinPoolDiversity <= 0 {
		cfg.MinPoolDiversity = cfg.MinEliteDiversity
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = "ai_weights"
	}
	if cfg.OpponentMode == "" {
		cfg.OpponentMode = "random"
	}
	return cfg
}
