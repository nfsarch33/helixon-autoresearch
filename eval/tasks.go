package eval

import "fmt"

// TaskTypeID is a stable identifier for a task category.
type TaskTypeID string

// The seven agent-centric task types required by Sprint B. Each type
// exercises a distinct agent capability so the comparative matrix
// surfaces per-capability backend differences.
const (
	TaskCodeGeneration  TaskTypeID = "code_generation"
	TaskCodeReview      TaskTypeID = "code_review"
	TaskDebugging       TaskTypeID = "debugging"
	TaskDocumentation   TaskTypeID = "documentation"
	TaskLongContext     TaskTypeID = "long_context"
	TaskSelfImprovement TaskTypeID = "self_improvement"
	TaskMultiStepPlan   TaskTypeID = "multi_step_planning"
)

// Difficulty scales a task's complexity and is surfaced in the report so
// we can detect backend cliffs at higher difficulty.
type Difficulty string

const (
	DiffEasy   Difficulty = "easy"
	DiffMedium Difficulty = "medium"
	DiffHard   Difficulty = "hard"
)

// Task is a single unit of work presented to a Helixon agent. The
// agent receives the Prompt and any ContextFiles and must produce an
// Output that the judge scores against the task's rubric.
type Task struct {
	ID         string     `json:"id"`
	Type       TaskTypeID `json:"type"`
	Difficulty Difficulty `json:"difficulty"`
	Name       string     `json:"name"`
	Prompt     string     `json:"prompt"`
	// ContextFiles are optional file contents the agent may use. Long-context
	// and code-review tasks populate this; others may leave it empty.
	ContextFiles []ContextFile `json:"context_files,omitempty"`
	// ExpectedOutput is a loose gold reference used by the judge for
	// reference-based scoring. It is NOT used as a strict string match.
	ExpectedOutput string `json:"expected_output,omitempty"`
	// MaxTurns caps the agent conversation length. Long-session tasks set
	// this high so stability can be measured across many turns.
	MaxTurns int `json:"max_turns,omitempty"`
}

// ContextFile is a file surfaced to the agent as context.
type ContextFile struct {
	Path     string `json:"path"`
	Language string `json:"language,omitempty"`
	Content  string `json:"content"`
}

// DefaultTaskSuite returns the canonical seven-task suite covering all
// required task types at medium difficulty. The suite is deterministic
// so runs are reproducible and comparable across backends.
func DefaultTaskSuite() []Task {
	return []Task{
		taskCodeGenerationMedium(),
		taskCodeReviewMedium(),
		taskDebuggingMedium(),
		taskDocumentationMedium(),
		taskLongContextMedium(),
		taskSelfImprovementMedium(),
		taskMultiStepPlanMedium(),
	}
}

// TaskByID looks up a task in a suite by ID.
func TaskByID(tasks []Task, id string) (Task, bool) {
	for _, t := range tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// TasksByType filters a suite to a single task type.
func TasksByType(tasks []Task, t TaskTypeID) []Task {
	var out []Task
	for _, task := range tasks {
		if task.Type == t {
			out = append(out, task)
		}
	}
	return out
}

// --- Task definitions ---

func taskCodeGenerationMedium() Task {
	return Task{
		ID:         "cg-001",
		Type:       TaskCodeGeneration,
		Difficulty: DiffMedium,
		Name:       "Generate a rate-limited HTTP client wrapper",
		Prompt: `Write a Go package ` + "`ratelimit`" + ` that wraps an *http.Client with a
token-bucket rate limiter. Requirements:
- Constructor NewLimiterClient(base *http.Client, ratePerSec float64, burst int) *LimiterClient
- The returned client must satisfy the ` + "`Do(req *http.Request) (*http.Response, error)`" + ` contract
- Thread-safe; concurrent callers must share the bucket
- Context-aware: cancel the wait if req.Context() is cancelled
- Include a unit test with a fake clock or short real sleep
Return only the code, no prose.`,
		ExpectedOutput: `A Go file declaring package ratelimit with a LimiterClient struct,
a token-bucket implementation (golang.org/x/time/rate or hand-rolled),
context-aware Do method, sync.Mutex or channel-based safety, and a
_test.go file with at least one passing test.`,
		MaxTurns: 1,
	}
}

func taskCodeReviewMedium() Task {
	return Task{
		ID:         "cr-001",
		Type:       TaskCodeReview,
		Difficulty: DiffMedium,
		Name:       "Review a PR with a concurrency bug",
		Prompt: `Review the following Go PR. Identify correctness, concurrency,
and security issues. Provide numbered findings with severity
(critical/major/minor) and a suggested fix for each.`,
		ContextFiles: []ContextFile{
			{
				Path:     "cache.go",
				Language: "go",
				Content: `package cache

type Cache struct {
	m map[string]string
}

func New() *Cache { return &Cache{m: make(map[string]string)} }

func (c *Cache) Get(k string) string {
	return c.m[k]
}

func (c *Cache) Set(k, v string) {
	c.m[k] = v
}

func (c *Cache) Delete(k string) {
	delete(c.m, k)
}
`,
			},
		},
		ExpectedOutput: `Findings should flag: (1) CRITICAL - concurrent map access with no
mutex; (2) MAJOR - no sync.RWMutex or sync.Map; (3) MINOR - Get returns
zero-value silently on miss. Suggested fix: add sync.RWMutex, Lock on
Set/Delete, RLock on Get, and consider a bool-returning Get.`,
		MaxTurns: 1,
	}
}

func taskDebuggingMedium() Task {
	return Task{
		ID:         "db-001",
		Type:       TaskDebugging,
		Difficulty: DiffMedium,
		Name:       "Find and fix a nil-pointer dereference",
		Prompt: `The following program panics at runtime with a nil pointer
dereference. Identify the root cause, explain why it happens, and
provide a corrected version.`,
		ContextFiles: []ContextFile{
			{
				Path:     "main.go",
				Language: "go",
				Content: `package main

import "fmt"

type User struct {
	Name    string
	Address *Address
}

type Address struct {
	City string
}

func cityFor(u *User) string {
	return u.Address.City
}

func main() {
	u := &User{Name: "alice"}
	fmt.Println(cityFor(u))
}
`,
			},
		},
		ExpectedOutput: `Root cause: User.Address is nil because it was never set, so
u.Address.City dereferences a nil pointer. Fix: either initialize
Address, or guard with ` + "`if u.Address != nil`" + ` in cityFor and
return a default city.`,
		MaxTurns: 2,
	}
}

func taskDocumentationMedium() Task {
	return Task{
		ID:         "dc-001",
		Type:       TaskDocumentation,
		Difficulty: DiffMedium,
		Name:       "Document an undocumented function",
		Prompt: `Write godoc-compatible documentation for the following function.
Include a package-level example and document every parameter, return
value, and error case. Output only the documented code.`,
		ContextFiles: []ContextFile{
			{
				Path:     "merge.go",
				Language: "go",
				Content: `package merge

func MergeMaps(dst, src map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(dst))
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		if existing, ok := out[k]; ok {
			if eMap, ok1 := existing.(map[string]interface{}); ok1 {
				if sMap, ok2 := v.(map[string]interface{}); ok2 {
					out[k] = MergeMaps(eMap, sMap)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}
`,
			},
		},
		ExpectedOutput: `A documented version with: package comment, function comment
explaining recursive deep-merge semantics, parameter docs, return
value doc, and an ExampleMergeMaps function.`,
		MaxTurns: 1,
	}
}

func taskLongContextMedium() Task {
	return Task{
		ID:         "lc-001",
		Type:       TaskLongContext,
		Difficulty: DiffMedium,
		Name:       "Summarize and answer questions across a large codebase",
		Prompt: `You are given multiple Go files from a small service. First,
summarize the service's responsibilities in 5 bullets. Then answer:
(1) What is the single source of truth for configuration?
(2) Which function performs retry? (3) Where are metrics emitted?
Cite file:line for each answer.`,
		ContextFiles: longContextSampleFiles(),
		ExpectedOutput: `A 5-bullet summary plus three answers, each citing file:line.
Quality depends on correctly cross-referencing the files rather than
hallucinating.`,
		MaxTurns: 3,
	}
}

func taskSelfImprovementMedium() Task {
	return Task{
		ID:         "si-001",
		Type:       TaskSelfImprovement,
		Difficulty: DiffMedium,
		Name:       "Identify and fix a mistake in your own prior output",
		Prompt: `Below is your previous answer to a coding task, followed by a
hint that it contains a bug. Re-examine your answer, identify the
mistake, explain it, and produce a corrected version.

PREVIOUS ANSWER:
` + "`" + `func Sum(nums []int) int {
	var total int
	for i := 1; i < len(nums); i++ {
		total += nums[i]
	}
	return total
}` + "`" + `

HINT: The first element is never included.
Produce: (1) the identified mistake, (2) why it happened,
(3) the corrected code.`,
		ExpectedOutput: `Mistake: loop starts at i=1, skipping nums[0]. Why: off-by-one
error in range initialization. Fix: start at i=0, or use ` + "`for _, n := range nums`" + `.`,
		MaxTurns: 2,
	}
}

func taskMultiStepPlanMedium() Task {
	return Task{
		ID:         "mp-001",
		Type:       TaskMultiStepPlan,
		Difficulty: DiffMedium,
		Name:       "Plan a canary deployment rollout",
		Prompt: `Produce a step-by-step plan to canary-deploy a new version of
a Go HTTP service behind a load balancer. Cover: health checks,
traffic shifting (10% -> 50% -> 100%), rollback triggers, metric
watch points, and the human-approval gate. Output an ordered numbered
list with a success criterion and rollback action per step. Stay
coherent across all steps.`,
		ExpectedOutput: `An ordered plan of 6-10 steps, each with: action, success
criterion, and rollback trigger. Steps should be internally consistent
and reference shared metrics (e.g. error rate, p99 latency).`,
		MaxTurns: 5,
	}
}

// longContextSampleFiles returns several small synthetic files that
// together exercise cross-file reasoning without a huge token cost.
func longContextSampleFiles() []ContextFile {
	return []ContextFile{
		{
			Path:     "cmd/server/main.go",
			Language: "go",
			Content: `package main

import (
	"flag"
	"log"
	"myapp/internal/config"
	"myapp/internal/server"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config")
	flag.Parse()
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	srv := server.New(cfg)
	log.Fatal(srv.ListenAndServe())
}
`,
		},
		{
			Path:     "internal/config/config.go",
			Language: "go",
			Content: `package config

import ("os"; "gopkg.in/yaml.v3")

type Config struct {
	HTTPAddr string ` + "`" + `yaml:"http_addr"` + "`" + `
	MaxRetry int    ` + "`" + `yaml:"max_retry"` + "`" + `
}

// Load is the single source of truth for runtime configuration.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil { return nil, err }
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil { return nil, err }
	return &c, nil
}
`,
		},
		{
			Path:     "internal/server/retry.go",
			Language: "go",
			Content: `package server

import ("context"; "time")

// DoWithRetry retries op up to max times with exponential backoff.
func DoWithRetry(ctx context.Context, max int, op func() error) error {
	var err error
	for i := 0; i < max; i++ {
		if err = op(); err == nil { return nil }
		select {
		case <-ctx.Done(): return ctx.Err()
		case <-time.After(time.Duration(1<<uint(i)) * time.Second):
		}
	}
	return err
}
`,
		},
		{
			Path:     "internal/server/metrics.go",
			Language: "go",
			Content: `package server

import "myapp/internal/metrics"

func (s *Server) recordRequest(status int, ms float64) {
	metrics.RequestLatency.Observe(ms)
	metrics.RequestCount(status).Inc()
}
`,
		},
		{
			Path:     "internal/metrics/metrics.go",
			Language: "go",
			Content: `package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	RequestLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "http_request_duration_ms",
	})
	reqCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
	}, []string{"status"})
)

func RequestCount(status int) prometheus.Counter {
	return reqCounts.WithLabelValues(itoa(status))
}
`,
		},
	}
}

// String returns a human-readable label for a task type.
func (t TaskTypeID) String() string {
	switch t {
	case TaskCodeGeneration:
		return "Code Generation"
	case TaskCodeReview:
		return "Code Review"
	case TaskDebugging:
		return "Debugging"
	case TaskDocumentation:
		return "Documentation"
	case TaskLongContext:
		return "Long Context"
	case TaskSelfImprovement:
		return "Self-Improvement"
	case TaskMultiStepPlan:
		return "Multi-Step Planning"
	default:
		return fmt.Sprintf("unknown(%s)", string(t))
	}
}

// AllTaskTypes returns the seven task types in canonical order.
func AllTaskTypes() []TaskTypeID {
	return []TaskTypeID{
		TaskCodeGeneration,
		TaskCodeReview,
		TaskDebugging,
		TaskDocumentation,
		TaskLongContext,
		TaskSelfImprovement,
		TaskMultiStepPlan,
	}
}
