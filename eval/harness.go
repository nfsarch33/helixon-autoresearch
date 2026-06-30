package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// LLMBackend describes a backend the Helixon agent runs on. Backends
// are varied to measure how the SAME agent performs across them; this
// is agent-centric evaluation, not a model-vs-model comparison.
type LLMBackend struct {
	Name   string `json:"name"`    // human label, e.g. "aliyun-qwen3.7-plus"
	Model  string `json:"model"`   // model id sent to the API, e.g. "qwen3.7-plus"
	APIURL string `json:"api_url"` // OpenAI-compatible base URL
	APIKey string `json:"-"`       // resolved from 1Password at runtime, never serialized
	Router string `json:"router"`  // optional llm-cluster-router URL; if set, APIURL is ignored
}

// DefaultBackends returns the three Sprint B backends. API keys are
// left empty and MUST be populated by the caller from 1Password before
// Run() is invoked.
func DefaultBackends() []LLMBackend {
	return []LLMBackend{
		{
			Name:   "aliyun-qwen3.7-plus",
			Model:  "qwen3.7-plus",
			APIURL: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
			// Aliyun cn-beijing endpoint, NOT the international one.
		},
		{
			Name:   "aliyun-qwen3.7-max",
			Model:  "qwen3.7-max",
			APIURL: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		},
		{
			Name:   "minimax-m3",
			Model:  "MiniMax-M3",
			APIURL: "https://api.minimaxi.com/v1",
			// minimaxi.com, NOT minimax.io.
		},
	}
}

// chatMessage is the OpenAI-compatible chat message shape.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the OpenAI-compatible chat completion request.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

// chatResponse is a minimal OpenAI-compatible chat completion response.
type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Complete sends a single-turn prompt to the backend and returns the
// assistant text plus token usage. A router URL, when set, takes
// precedence so traffic flows through the llm-cluster-router.
func (b LLMBackend) Complete(ctx context.Context, prompt string, maxTokens int) (text string, usage TokenUsage, err error) {
	req := chatRequest{
		Model:       b.Model,
		Messages:    []chatMessage{{Role: "user", Content: prompt}},
		MaxTokens:   maxTokens,
		Temperature: 0.0,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := strings.TrimRight(b.APIURL, "/") + "/chat/completions"
	if b.Router != "" {
		endpoint = strings.TrimRight(b.Router, "/") + "/v1/chat/completions"
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if b.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+b.APIKey)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("backend %s request: %w", b.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", TokenUsage{}, fmt.Errorf("backend %s returned %d: %s", b.Name, resp.StatusCode, string(raw))
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", TokenUsage{}, fmt.Errorf("decode response from %s: %w", b.Name, err)
	}
	if len(cr.Choices) == 0 {
		return "", TokenUsage{}, fmt.Errorf("backend %s returned no choices", b.Name)
	}

	usage = TokenUsage{
		Prompt:     cr.Usage.PromptTokens,
		Completion: cr.Usage.CompletionTokens,
		Total:      cr.Usage.TotalTokens,
	}
	return cr.Choices[0].Message.Content, usage, nil
}

// TokenUsage records token counts per completion for efficiency scoring.
type TokenUsage struct {
	Prompt     int `json:"prompt"`
	Completion int `json:"completion"`
	Total      int `json:"total"`
}

// EvalHarness runs a task suite across multiple LLM backends and scores
// each output with the G-Eval LLM-as-judge pattern. The Judge backend
// evaluates the agent backends' outputs; to avoid self-preference bias
// the judge SHOULD be a different backend from the one being judged.
type EvalHarness struct {
	Backends []LLMBackend
	Tasks    []Task
	Rubrics  map[TaskTypeID]*Rubric
	Judge    LLMBackend
	Logger   *slog.Logger
}

// NewEvalHarness builds a harness with the default suite, rubrics, and
// the given backends. The Judge is set to the first backend by default
// but should be overridden via WithJudge for production runs.
func NewEvalHarness(backends []LLMBackend) *EvalHarness {
	return &EvalHarness{
		Backends: backends,
		Tasks:    DefaultTaskSuite(),
		Rubrics:  DefaultRubrics(),
		Logger:   slog.Default(),
	}
}

// WithJudge sets the G-Eval judge backend.
func (h *EvalHarness) WithJudge(j LLMBackend) *EvalHarness {
	h.Judge = j
	return h
}

// WithLogger sets the structured logger.
func (h *EvalHarness) WithLogger(l *slog.Logger) *EvalHarness {
	if l != nil {
		h.Logger = l
	}
	return h
}

// WithTasks overrides the default task suite.
func (h *EvalHarness) WithTasks(tasks []Task) *EvalHarness {
	h.Tasks = tasks
	return h
}

// Run executes every task on every backend, scores each output with the
// G-Eval judge, and returns a full comparative report. Each backend
// task has a per-call timeout to bound total runtime.
func (h *EvalHarness) Run(ctx context.Context) (*EvalReport, error) {
	if len(h.Backends) == 0 {
		return nil, fmt.Errorf("no backends configured")
	}
	if len(h.Tasks) == 0 {
		return nil, fmt.Errorf("no tasks configured")
	}
	if h.Judge.Model == "" {
		return nil, fmt.Errorf("judge backend not configured")
	}
	for _, b := range h.Backends {
		if b.APIKey == "" && b.Router == "" {
			return nil, fmt.Errorf("backend %q has no API key and no router; resolve keys from 1Password first", b.Name)
		}
	}

	h.Logger.Info("starting eval run",
		"backends", len(h.Backends),
		"tasks", len(h.Tasks),
		"judge", h.Judge.Name,
		"rubric_version", RubricVersion,
	)

	report := &EvalReport{
		Timestamp:     time.Now().UTC(),
		RubricVersion: RubricVersion,
		Results:       []TaskResult{},
	}

	for _, task := range h.Tasks {
		rubric, ok := h.Rubrics[task.Type]
		if !ok {
			return nil, fmt.Errorf("no rubric for task type %q", task.Type)
		}
		if err := rubric.Validate(); err != nil {
			return nil, fmt.Errorf("rubric for %s invalid: %w", task.Type, err)
		}

		for _, backend := range h.Backends {
			result := h.runOne(ctx, task, rubric, backend)
			report.Results = append(report.Results, result)
		}
	}

	report.Matrix = buildMatrix(report.Results, h.Backends, h.Tasks)
	report.Summary = buildSummary(report.Results, h.Backends)

	h.Logger.Info("eval run complete",
		"total_results", len(report.Results),
		"matrix_backends", len(report.Matrix.ByBackend),
		"overall_verdict", report.Summary.OverallVerdict,
	)

	return report, nil
}

// runOne executes a single task on a single backend and judges it.
// The agent and judge calls are independently timeout-bounded.
func (h *EvalHarness) runOne(ctx context.Context, task Task, rubric *Rubric, backend LLMBackend) TaskResult {
	start := time.Now()
	result := TaskResult{
		TaskID:   task.ID,
		TaskType: task.Type,
		TaskName: task.Name,
		Backend:  backend.Name,
		Model:    backend.Model,
		Rubric:   rubric.Name,
	}

	agentPrompt := buildAgentPrompt(task)
	agentCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	output, usage, err := backend.Complete(agentCtx, agentPrompt, 2048)
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		result.AgentOutput = ""
		h.Logger.Error("agent completion failed",
			"task", task.ID, "backend", backend.Name, "err", err)
		// A failed run still produces zero scores so the matrix stays complete.
		result.CriterionScores = zeroScores(rubric)
		result.Verdict = "RED"
		return result
	}

	result.AgentOutput = output
	result.TokenUsage = usage

	// G-Eval: judge scores the agent's output against the rubric.
	judgeCtx, jcancel := context.WithTimeout(ctx, 120*time.Second)
	defer jcancel()
	scores, judgeErr := h.judgeScore(judgeCtx, task, rubric, output)
	if judgeErr != nil {
		h.Logger.Error("judge scoring failed; falling back to zero",
			"task", task.ID, "backend", backend.Name, "err", judgeErr)
		result.Error = "judge: " + judgeErr.Error()
		result.CriterionScores = zeroScores(rubric)
		result.Verdict = "RED"
		return result
	}

	result.CriterionScores = scores
	result.WeightedScore = weightedScore(rubric, scores)
	result.Verdict = verdictFromScore(result.WeightedScore)
	return result
}

// buildAgentPrompt assembles the full prompt shown to the agent,
// including context files and the task brief.
func buildAgentPrompt(task Task) string {
	var sb strings.Builder
	sb.WriteString("You are a Helixon agent. Complete the following task.\n\n")
	if len(task.ContextFiles) > 0 {
		sb.WriteString("=== CONTEXT FILES ===\n")
		for _, f := range task.ContextFiles {
			sb.WriteString(fmt.Sprintf("--- %s ---\n```%s\n%s\n```\n", f.Path, f.Language, f.Content))
		}
		sb.WriteString("=== END CONTEXT FILES ===\n\n")
	}
	sb.WriteString("=== TASK ===\n")
	sb.WriteString(task.Prompt)
	sb.WriteString("\n=== END TASK ===\n")
	return sb.String()
}

// judgeScore implements the G-Eval LLM-as-judge pattern. It asks the
// judge backend to score the agent output on each rubric criterion using
// the anchored 1-5 scale, then parses the structured JSON response.
func (h *EvalHarness) judgeScore(ctx context.Context, task Task, rubric *Rubric, agentOutput string) (map[CriterionID]Score, error) {
	prompt := buildJudgePrompt(task, rubric, agentOutput)
	raw, _, err := h.Judge.Complete(ctx, prompt, 1024)
	if err != nil {
		return nil, fmt.Errorf("judge complete: %w", err)
	}

	scores, err := parseJudgeResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse judge response: %w (raw: %q)", err, truncate(raw, 200))
	}

	// Validate every rubric criterion received a score.
	for _, c := range rubric.Criteria {
		if _, ok := scores[c.ID]; !ok {
			return nil, fmt.Errorf("judge response missing criterion %q", c.ID)
		}
	}
	return scores, nil
}

// Score is a single criterion score with the judge's rationale.
type Score struct {
	Value     int    `json:"value"` // 1..ScaleMax
	Rationale string `json:"rationale"`
}

// buildJudgePrompt constructs the G-Eval prompt with anchored rubric
// levels and a strict JSON output contract.
func buildJudgePrompt(task Task, rubric *Rubric, agentOutput string) string {
	var sb strings.Builder
	sb.WriteString("You are a strict evaluator scoring an AI agent's output. ")
	sb.WriteString("Score each criterion on a 1-5 integer scale using the anchors. ")
	sb.WriteString("Respond ONLY with a JSON object, no prose, matching this schema:\n")
	sb.WriteString(`{"criterion_id":{"value":1-5,"rationale":"short"},...}` + "\n\n")
	sb.WriteString("=== TASK ===\n" + task.Prompt + "\n\n")
	sb.WriteString("=== EXPECTED (reference, not strict match) ===\n" + task.ExpectedOutput + "\n\n")
	sb.WriteString("=== AGENT OUTPUT ===\n" + agentOutput + "\n\n")
	sb.WriteString("=== RUBRIC ===\n")
	for _, c := range rubric.Criteria {
		sb.WriteString(fmt.Sprintf("- %s (%s, weight %.2f): %s\n", c.ID, c.Name, c.Weight, c.Description))
		sb.WriteString("  " + c.AnchorLow + "\n")
		sb.WriteString("  " + c.AnchorMid + "\n")
		sb.WriteString("  " + c.AnchorHigh + "\n")
	}
	sb.WriteString("\nReturn the JSON object now.")
	return sb.String()
}

// parseJudgeResponse extracts the criterion scores from the judge's
// JSON response, tolerating surrounding code fences, <think>...</think>
// reasoning prefixes (MiniMax-M3 default), and prose wrappers.
func parseJudgeResponse(rawResp string) (map[CriterionID]Score, error) {
	cleaned := stripCodeFence(rawResp)
	cleaned = stripThinkTag(cleaned)
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in judge response")
	}
	jsonStr := cleaned[start : end+1]

	type judgeScore struct {
		Value     int    `json:"value"`
		Rationale string `json:"rationale"`
	}
	parsed := make(map[string]judgeScore)
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	out := make(map[CriterionID]Score, len(parsed))
	for k, js := range parsed {
		v := js.Value
		if v < 1 {
			v = 1
		}
		if v > ScaleMax {
			v = ScaleMax
		}
		out[CriterionID(k)] = Score{Value: v, Rationale: js.Rationale}
	}
	return out, nil
}

// stripCodeFence removes ```json ... ``` wrappers if present.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if nl := strings.Index(s, "\n"); nl > 0 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

// stripThinkTag removes a leading <think>... reasoning block that some
// chat models (notably MiniMax-M3) emit before their final answer. If the
// tag is unclosed (model ran out of tokens), only the leading <think>
// token is removed so the parser can still recover the JSON that follows.
func stripThinkTag(s string) string {
	const openTag = "<think>"
	const closeTag = "</think>"
	openIdx := strings.Index(s, openTag)
	if openIdx < 0 {
		return s
	}
	closeIdx := strings.Index(s[openIdx+len(openTag):], closeTag)
	if closeIdx < 0 {
		// Unclosed: drop everything up to the first '{' after the open tag.
		brace := strings.Index(s[openIdx+len(openTag):], "{")
		if brace < 0 {
			return ""
		}
		return s[openIdx+len(openTag)+brace:]
	}
	afterClose := openIdx + len(openTag) + closeIdx + len(closeTag)
	return strings.TrimSpace(s[afterClose:])
}

// zeroScores returns minimum scores for a failed run.
func zeroScores(rubric *Rubric) map[CriterionID]Score {
	out := make(map[CriterionID]Score, len(rubric.Criteria))
	for _, c := range rubric.Criteria {
		out[c.ID] = Score{Value: 1, Rationale: "agent or judge failed; floor score"}
	}
	return out
}

// weightedScore computes the weighted average score as a 0-100 value.
func weightedScore(rubric *Rubric, scores map[CriterionID]Score) float64 {
	var total float64
	for _, c := range rubric.Criteria {
		s := scores[c.ID]
		total += (float64(s.Value) / float64(ScaleMax)) * c.Weight
	}
	return total * 100
}

// verdictFromScore maps a 0-100 weighted score to a traffic-light verdict.
func verdictFromScore(score float64) string {
	switch {
	case score >= 80:
		return "GREEN"
	case score >= 50:
		return "YELLOW"
	default:
		return "RED"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
