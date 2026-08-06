package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/nfsarch33/helixon-autoresearch/internal/autoresearch"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	switch os.Args[1] {
	case "run":
		cmdRun(logger)
	case "history":
		cmdHistory(logger)
	case "search":
		cmdSearch(logger)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `autoresearch - Autonomous experiment loop CLI

Commands:
  run <name> <hypothesis>     Run a new experiment
  history <experiment-id>     Show experiment history
  search <query> [limit]      Search related experiments

Environment:
  ENGRAM_URL       Engram API base URL (default: http://127.0.0.1:<port>)
  ENGRAM_APP_ID    Engram app ID (default: autoresearch)
  ENGRAM_USER_ID   Engram user ID (default: nfsarch33)
`)
}

func buildClient() autoresearch.EngramClient {
	return autoresearch.NewHTTPEngramClient(autoresearch.EngramClientConfig{
		BaseURL: os.Getenv("ENGRAM_URL"),
		AppID:   os.Getenv("ENGRAM_APP_ID"),
		UserID:  os.Getenv("ENGRAM_USER_ID"),
	})
}

func cmdRun(logger *slog.Logger) {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: autoresearch run <name> <hypothesis>")
		os.Exit(1)
	}

	client := buildClient()
	loop := autoresearch.NewExperimentLoop(client, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := loop.RunExperiment(ctx, autoresearch.ExperimentConfig{
		Name:       os.Args[2],
		Hypothesis: os.Args[3],
	})
	if err != nil {
		logger.Error("experiment failed", "err", err)
		os.Exit(1)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}

func cmdHistory(logger *slog.Logger) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: autoresearch history <experiment-id>")
		os.Exit(1)
	}

	client := buildClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	memories, err := client.GetExperimentHistory(ctx, os.Args[2])
	if err != nil {
		logger.Error("failed to get history", "err", err)
		os.Exit(1)
	}

	data, _ := json.MarshalIndent(memories, "", "  ")
	fmt.Println(string(data))
}

func cmdSearch(logger *slog.Logger) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: autoresearch search <query> [limit]")
		os.Exit(1)
	}

	limit := 10
	if len(os.Args) >= 4 {
		if n, err := strconv.Atoi(os.Args[3]); err == nil && n > 0 {
			limit = n
		}
	}

	client := buildClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	memories, err := client.SearchRelatedExperiments(ctx, os.Args[2], limit)
	if err != nil {
		logger.Error("search failed", "err", err)
		os.Exit(1)
	}

	data, _ := json.MarshalIndent(memories, "", "  ")
	fmt.Println(string(data))
}
