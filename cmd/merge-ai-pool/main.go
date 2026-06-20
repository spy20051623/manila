package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"manila/internal/training"
)

type mergeReport struct {
	PoolSize       int                        `json:"poolSize"`
	MinDiversity   float64                    `json:"minDiversity"`
	BehaviorWeight float64                    `json:"behaviorWeight"`
	Selected       []training.GenomePoolEntry `json:"selected"`
	ForwardAverage float64                    `json:"forwardAverageScore"`
	ReverseAverage float64                    `json:"reverseOldSoloScore"`
}

func main() {
	var evalPath string
	var outDir string
	var cfg training.PoolMergeConfig
	flag.StringVar(&evalPath, "eval", "", "pool evaluation JSON produced by cmd/evaluate-ai")
	flag.StringVar(&outDir, "out", "ai_weights_current", "output directory for merged pool")
	flag.IntVar(&cfg.PoolSize, "poolSize", 8, "number of genomes to keep")
	flag.Float64Var(&cfg.MinDiversity, "minDiversity", 0.20, "minimum combined distance between selected genomes")
	flag.Float64Var(&cfg.BehaviorWeight, "behaviorWeight", 0.35, "how much behavior distance contributes to diversity")
	flag.Parse()

	if evalPath == "" {
		fmt.Fprintln(os.Stderr, "-eval is required")
		os.Exit(1)
	}
	result, err := loadPoolEvaluation(evalPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load evaluation: %v\n", err)
		os.Exit(1)
	}
	pool := training.MergeEvaluatedPools(result, cfg)
	if len(pool.Entries) == 0 {
		fmt.Fprintln(os.Stderr, "evaluation contains no candidate genomes")
		os.Exit(1)
	}
	if err := training.WriteGenomePoolArtifacts(outDir, pool); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write merged pool: %v\n", err)
		os.Exit(1)
	}
	report := mergeReport{
		PoolSize:       len(pool.Entries),
		MinDiversity:   cfg.MinDiversity,
		BehaviorWeight: cfg.BehaviorWeight,
		Selected:       pool.Entries,
		ForwardAverage: result.ForwardAverageScore,
		ReverseAverage: result.ReverseOldSoloScore,
	}
	if err := writeJSON(filepath.Join(outDir, "pool_merge_result.json"), report); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write merge report: %v\n", err)
		os.Exit(1)
	}
	for _, entry := range pool.Entries {
		fmt.Printf("%s score=%.4f\n", entry.ID, entry.Score)
	}
}

func loadPoolEvaluation(path string) (training.PoolEvaluationResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return training.PoolEvaluationResult{}, err
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var result training.PoolEvaluationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return training.PoolEvaluationResult{}, err
	}
	return result, nil
}

func writeJSON(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
