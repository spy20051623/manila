package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"manila/internal/training"
)

func main() {
	var genomePath string
	var genomePoolPath string
	var baselineGenomePath string
	var baselinePoolPath string
	var matchMode string
	var cfg training.EvaluationConfig
	flag.StringVar(&genomePath, "genome", "", "candidate genome JSON")
	flag.StringVar(&genomePoolPath, "genomePool", "", "candidate genome pool JSON or directory")
	flag.StringVar(&baselineGenomePath, "baselineGenome", "", "optional baseline genome JSON for mixed opponents")
	flag.StringVar(&baselinePoolPath, "baselinePool", "", "optional baseline genome pool JSON or directory for pool opponents")
	flag.StringVar(&matchMode, "match", "both", "pool match mode: forward, reverse, or both")
	flag.IntVar(&cfg.SeedsPerSeat, "seeds", 8, "evaluation seeds per seat")
	flag.IntVar(&cfg.MaxSteps, "maxSteps", 600, "maximum actions per game")
	flag.Int64Var(&cfg.BaseSeed, "seed", 1, "base random seed")
	flag.StringVar(&cfg.OpponentMode, "opponents", "random", "opponent pool: random, mixed, pool, or blended")
	flag.Parse()

	if genomePath == "" && genomePoolPath == "" {
		fmt.Fprintln(os.Stderr, "-genome or -genomePool is required")
		os.Exit(1)
	}
	if baselineGenomePath != "" {
		baseline, err := training.LoadGenome(baselineGenomePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load baseline genome: %v\n", err)
			os.Exit(1)
		}
		cfg.BaselineGenome = &baseline
	}
	if baselinePoolPath != "" {
		pool, err := training.LoadGenomePool(baselinePoolPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load baseline pool: %v\n", err)
			os.Exit(1)
		}
		cfg.BaselinePool = pool
		if cfg.OpponentMode == "random" {
			cfg.OpponentMode = "pool"
		}
	}
	var result interface{}
	if genomePoolPath != "" {
		candidates, err := training.LoadGenomePool(genomePoolPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load genome pool: %v\n", err)
			os.Exit(1)
		}
		if len(cfg.BaselinePool) == 0 {
			fmt.Fprintln(os.Stderr, "-baselinePool is required when using -genomePool")
			os.Exit(1)
		}
		result = training.EvaluateGenomePool(candidates, cfg.BaselinePool, cfg, matchMode)
	} else {
		genome, err := training.LoadGenome(genomePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load genome: %v\n", err)
			os.Exit(1)
		}
		result = training.EvaluateGenome(genome, cfg)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}
