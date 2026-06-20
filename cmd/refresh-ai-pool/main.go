package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"manila/internal/ai"
	"manila/internal/training"
)

type refreshReport struct {
	CandidatePool string                        `json:"candidatePool"`
	CurrentPool   string                        `json:"currentPool"`
	BackupDir     string                        `json:"backupDir"`
	OutputDir     string                        `json:"outputDir"`
	Approved      bool                          `json:"approved"`
	QuickEval     training.PoolEvaluationResult `json:"quickEval"`
	FinalEval     training.PoolEvaluationResult `json:"finalEval"`
	Selected      []training.GenomePoolEntry    `json:"selected"`
}

func main() {
	var candidateDir string
	var currentDir string
	var outDir string
	var quickSeeds int
	var finalSeeds int
	var maxSteps int
	var poolSize int
	var minCombinedAdvantage float64
	flag.StringVar(&candidateDir, "candidatePool", "", "new candidate genome pool JSON or directory")
	flag.StringVar(&currentDir, "currentPool", "ai_weights_current", "current genome pool JSON or directory")
	flag.StringVar(&outDir, "out", "ai_weights_current", "output directory to update")
	flag.IntVar(&quickSeeds, "quickSeeds", 2, "quick evaluation seeds per seat")
	flag.IntVar(&finalSeeds, "finalSeeds", 4, "final confirmation seeds per seat")
	flag.IntVar(&maxSteps, "maxSteps", 600, "maximum actions per game")
	flag.IntVar(&poolSize, "poolSize", 8, "number of genomes to keep")
	flag.Float64Var(&minCombinedAdvantage, "minCombinedAdvantage", 0, "minimum final combined advantage required before writing to current pool")
	flag.Parse()

	if candidateDir == "" {
		fmt.Fprintln(os.Stderr, "-candidatePool is required")
		os.Exit(1)
	}
	candidates, err := training.LoadGenomePool(candidateDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load candidate pool: %v\n", err)
		os.Exit(1)
	}
	current, err := training.LoadGenomePool(currentDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load current pool: %v\n", err)
		os.Exit(1)
	}

	allCandidates := append([]ai.Genome{}, current...)
	allCandidates = append(allCandidates, candidates...)
	quick := training.EvaluateGenomePool(allCandidates, current, training.EvaluationConfig{
		SeedsPerSeat: quickSeeds,
		MaxSteps:     maxSteps,
		BaseSeed:     time.Now().UnixNano() % 1000000000,
	}, "forward")

	merged := training.MergeEvaluatedPools(quick, training.PoolMergeConfig{
		PoolSize:       poolSize,
		MinDiversity:   0.20,
		BehaviorWeight: 0.35,
	})
	if len(merged.Entries) == 0 {
		fmt.Fprintln(os.Stderr, "merged pool is empty")
		os.Exit(1)
	}

	mergedGenomes := make([]training.GenomePoolEntry, len(merged.Entries))
	copy(mergedGenomes, merged.Entries)
	final := training.EvaluateGenomePool(genomesFromEntries(merged.Entries), current, training.EvaluationConfig{
		SeedsPerSeat: finalSeeds,
		MaxSteps:     maxSteps,
		BaseSeed:     time.Now().UnixNano()%1000000000 + 500000000,
	}, "both")
	approved := final.CombinedAdvantage >= minCombinedAdvantage
	backupDir := ""
	if approved && samePath(outDir, currentDir) {
		backupDir = fmt.Sprintf("%s_backup_refresh_%s", outDir, time.Now().Format("20060102-150405"))
		if err := copyDir(outDir, backupDir); err != nil {
			fmt.Fprintf(os.Stderr, "failed to backup current pool: %v\n", err)
			os.Exit(1)
		}
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output dir: %v\n", err)
		os.Exit(1)
	}
	if approved {
		if err := training.WriteGenomePoolArtifacts(outDir, merged); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write merged pool: %v\n", err)
			os.Exit(1)
		}
	}
	report := refreshReport{
		CandidatePool: candidateDir,
		CurrentPool:   currentDir,
		BackupDir:     backupDir,
		OutputDir:     outDir,
		Approved:      approved,
		QuickEval:     quick,
		FinalEval:     final,
		Selected:      mergedGenomes,
	}
	if err := writeJSON(filepath.Join(outDir, "pool_refresh_result.json"), report); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write refresh report: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("quick combined advantage: %.4f\n", quick.CombinedAdvantage)
	fmt.Printf("final combined advantage: %.4f\n", final.CombinedAdvantage)
	if !approved {
		fmt.Printf("not approved: final combined advantage %.4f < %.4f\n", final.CombinedAdvantage, minCombinedAdvantage)
	}
	if backupDir != "" {
		fmt.Printf("backup: %s\n", backupDir)
	}
}

func genomesFromEntries(entries []training.GenomePoolEntry) []ai.Genome {
	genomes := make([]ai.Genome, 0, len(entries))
	for _, entry := range entries {
		genomes = append(genomes, entry.Genome)
	}
	return genomes
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && aa == bb
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func writeJSON(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
