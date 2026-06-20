package training

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"manila/internal/ai"
)

func TestRunRandomGamesReturnsSummaries(t *testing.T) {
	result := RunRandomGames(3, 10, 200)
	if len(result.Games) != 3 {
		t.Fatalf("expected 3 games, got %d", len(result.Games))
	}
	for _, game := range result.Games {
		if game.Steps == 0 {
			t.Fatalf("expected game to advance: %+v", game)
		}
		if game.Error != "" {
			t.Fatalf("random game errored: %+v", game)
		}
	}
}

func TestEvaluateGenomeIsReproducible(t *testing.T) {
	genome := ai.NeutralGenome()
	cfg := EvaluationConfig{SeedsPerSeat: 2, MaxSteps: 300, BaseSeed: 42}
	first := EvaluateGenome(genome, cfg)
	second := EvaluateGenome(genome, cfg)
	if first.AverageScore != second.AverageScore {
		t.Fatalf("expected reproducible score, got %f and %f", first.AverageScore, second.AverageScore)
	}
	if first.Games != 8 {
		t.Fatalf("expected 4 seats * 2 seeds, got %d", first.Games)
	}
	for seat := 1; seat <= 4; seat++ {
		if _, ok := first.SeatScores[seat]; !ok {
			t.Fatalf("missing seat score for seat %d", seat)
		}
	}
}

func TestEvaluateGenomeMixedOpponents(t *testing.T) {
	genome := ai.NeutralGenome()
	result := EvaluateGenome(genome, EvaluationConfig{
		SeedsPerSeat:   1,
		MaxSteps:       300,
		BaseSeed:       77,
		OpponentMode:   "mixed",
		BaselineGenome: &genome,
	})
	if result.Games != 4 {
		t.Fatalf("expected one game per seat, got %d", result.Games)
	}
	if len(result.Errors) > 0 {
		t.Fatalf("mixed evaluation errored: %+v", result.Errors)
	}
}

func TestTrainPopulationSmallRunWritesArtifacts(t *testing.T) {
	dir := t.TempDir()
	result, err := TrainPopulation(TrainConfig{
		Population:   4,
		Generations:  2,
		SeedsPerSeat: 1,
		MaxSteps:     300,
		BaseSeed:     11,
		OutputDir:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.History) != 2 {
		t.Fatalf("expected 2 generations, got %d", len(result.History))
	}
	for _, name := range []string{"best_genome.json", "best_weights.json", "genome_pool.json", "training_result.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected artifact %s: %v", name, err)
		}
	}
	if len(result.Pool.Entries) == 0 {
		t.Fatal("expected output genome pool")
	}
}

func TestLoadGenomePoolFallsBackToBestGenome(t *testing.T) {
	dir := t.TempDir()
	result, err := TrainPopulation(TrainConfig{
		Population:   4,
		Generations:  1,
		SeedsPerSeat: 1,
		MaxSteps:     300,
		BaseSeed:     12,
		OutputDir:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "genome_pool.json")); err != nil {
		t.Fatal(err)
	}
	pool, err := LoadGenomePool(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pool) != 1 || len(pool[0].Genes) != len(result.BestGenome.Genes) {
		t.Fatalf("expected fallback best genome, got %+v", pool)
	}
}

func TestEvaluateGenomePoolForwardAndReverse(t *testing.T) {
	candidates := []ai.Genome{ai.NeutralGenome(), ai.RandomGenome(rand.New(rand.NewSource(1)))}
	baselines := []ai.Genome{ai.RandomGenome(rand.New(rand.NewSource(2))), ai.RandomGenome(rand.New(rand.NewSource(3)))}
	result := EvaluateGenomePool(candidates, baselines, EvaluationConfig{
		SeedsPerSeat: 1,
		MaxSteps:     300,
		BaseSeed:     33,
	}, "both")
	if result.CandidateCount != 2 || result.BaselineCount != 2 {
		t.Fatalf("unexpected counts: %+v", result)
	}
	if len(result.ForwardResults) != 2 || len(result.ReverseBaselineResults) != 2 {
		t.Fatalf("expected forward and reverse results: %+v", result)
	}
}

func TestMutationFocusGenesAcceptNamesAndIndexes(t *testing.T) {
	genes, err := mutationFocusGenesFromConfig(TrainConfig{
		MutationFocus: []string{"auction_price_sensitivity,0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !genes[21] || !genes[0] {
		t.Fatalf("expected focused genes 21 and 0, got %+v", genes)
	}
}

func TestMutationFocusDecaysToNormal(t *testing.T) {
	cfg := normalizeTrainConfig(TrainConfig{
		Generations:                   5,
		MutationFocus:                 []string{"auction_price_sensitivity"},
		MutationResetProbability:      0.10,
		MutationFocusResetProbability: 0.50,
		MutationFocusSigmaMultiplier:  3.0,
	})
	genes, err := mutationFocusGenesFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first := mutationProfileForGeneration(cfg, genes, 1)
	middle := mutationProfileForGeneration(cfg, genes, 3)
	lastReproduction := mutationProfileForGeneration(cfg, genes, 4)
	if first.FocusResetProbability != 0.50 || first.FocusSigmaMultiplier != 3.0 {
		t.Fatalf("first generation should use full focus boost: %+v", first)
	}
	if middle.FocusResetProbability <= lastReproduction.FocusResetProbability || middle.FocusResetProbability >= first.FocusResetProbability {
		t.Fatalf("middle generation should decay between first and last: first=%+v middle=%+v last=%+v", first, middle, lastReproduction)
	}
	if lastReproduction.FocusResetProbability != cfg.MutationResetProbability || lastReproduction.FocusSigmaMultiplier != 1.0 {
		t.Fatalf("last reproduction should return to normal mutation: %+v", lastReproduction)
	}
}
