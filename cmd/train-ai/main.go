package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"manila/internal/ai"
	"manila/internal/training"
)

func main() {
	var cfg training.TrainConfig
	var baselineGenomePath string
	var baselinePoolPath string
	var mutationFocus string
	var listGenes bool
	flag.IntVar(&cfg.Population, "population", 24, "number of genomes per generation")
	flag.IntVar(&cfg.Generations, "generations", 8, "number of generations")
	flag.IntVar(&cfg.SeedsPerSeat, "seeds", 8, "evaluation seeds per seat")
	flag.IntVar(&cfg.MaxSteps, "maxSteps", 600, "maximum actions per game")
	flag.Int64Var(&cfg.BaseSeed, "seed", 1, "base random seed")
	flag.Float64Var(&cfg.MutationSigma, "mutationSigma", 0.12, "mutation standard deviation in genome space")
	flag.Float64Var(&cfg.MutationStart, "mutationStart", 0, "initial mutation standard deviation; defaults to mutationSigma")
	flag.Float64Var(&cfg.MutationEnd, "mutationEnd", 0, "final mutation standard deviation; defaults to mutationSigma")
	flag.StringVar(&mutationFocus, "mutationFocus", "", "comma-separated gene names or zero-based indexes that mutate more aggressively")
	flag.Float64Var(&cfg.MutationResetProbability, "mutationResetProbability", 0, "base random-reset probability per gene; defaults to 0.12")
	flag.Float64Var(&cfg.MutationFocusResetProbability, "mutationFocusResetProbability", 0, "random-reset probability for focused genes; defaults to 0.28")
	flag.Float64Var(&cfg.MutationFocusSigmaMultiplier, "mutationFocusSigmaMultiplier", 0, "sigma multiplier for focused genes; defaults to 2.0")
	flag.IntVar(&cfg.Workers, "workers", 0, "parallel evaluation workers; defaults to CPU count")
	flag.IntVar(&cfg.EliteCount, "eliteCount", 0, "number of elite genomes preserved each generation; defaults to about one third of population")
	flag.Float64Var(&cfg.MinEliteDiversity, "minEliteDiversity", 0, "minimum average gene distance between preserved elites; defaults to 0.18")
	flag.IntVar(&cfg.PoolSize, "poolSize", 0, "number of diverse genomes written to genome_pool.json; defaults to eliteCount")
	flag.Float64Var(&cfg.MinPoolDiversity, "minPoolDiversity", 0, "minimum average gene distance for output pool; defaults to minEliteDiversity")
	flag.StringVar(&cfg.OutputDir, "out", "ai_weights", "output directory")
	flag.StringVar(&cfg.OpponentMode, "opponents", "random", "opponent pool: random, mixed, pool, or blended")
	flag.StringVar(&baselineGenomePath, "baselineGenome", "", "optional baseline genome JSON for mixed opponents and population seeding")
	flag.StringVar(&baselinePoolPath, "baselinePool", "", "optional baseline genome pool JSON or directory for pool opponents")
	flag.BoolVar(&listGenes, "listGenes", false, "print gene indexes and names, then exit")
	flag.Parse()

	if listGenes {
		for i, name := range ai.GeneNames {
			fmt.Printf("%2d %s\n", i, name)
		}
		return
	}
	cfg.MutationFocus = splitCSV(mutationFocus)
	cfg.ProgressWriter = os.Stdout
	if baselineGenomePath != "" {
		genome, err := training.LoadGenome(baselineGenomePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load baseline genome: %v\n", err)
			os.Exit(1)
		}
		cfg.BaselineGenome = &genome
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
	result, err := training.TrainPopulation(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "training failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("best score: %.4f\n", result.BestScore)
	fmt.Printf("artifacts written to: %s\n", result.OutputDir)
	data, _ := json.MarshalIndent(result.BestWeights, "", "  ")
	fmt.Println(string(data))
}

func splitCSV(value string) []string {
	items := []string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}
