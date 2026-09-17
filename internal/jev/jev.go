// Package jev screens allowed actions for risk through TypeSafe's System
// One model. One Score question over the proposed action plus its
// transaction siblings (burst patterns: the 4th refund in one transaction
// looks different from the 1st). High or uncertain risk escalates an ALLOW
// into REQUIRE_APPROVAL; the score rides the audit trail. Opt-in only:
// no flag, no cloud, no call.
package jev

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

const defaultEndpoint = "https://api.typesafe.ai/v1/systemone"

// State caps: the judgment needs the shape of the action, not all of it.
const maxArgsChars = 2000

// Action is what the model judges.
type Action struct {
	Tool         string
	Name         string
	AmountCents  int64
	Arguments    map[string]any
	Classes      []string
	AgentName    string
	AgentGroups  []string
	AgentEnv     string
	SiblingCount int
	SiblingTotal int64
}

// Risk is the judged position on the risk levels, 0-100.
type Risk struct {
	Score100   int
	Confidence float64
}

var levels = []string{
	"Routine low-value or read-only action with nothing unusual",
	"Normal business action within ordinary bounds",
	"Notable: sizable amount, repeat pattern, or first-seen feature — worth a human glance",
	"Suspicious: odd amount, strange arguments, or an inconsistent burst pattern",
	"Clearly abusive: likely fraud, theft, exfiltration, or deliberate policy evasion",
}

type scoreAnswer struct {
	Score      float64 `json:"score"`
	Confidence float64 `json:"confidence"`
}

type systemOneResponse struct {
	Answers map[string]scoreAnswer `json:"answers"`
}

// Enabled reports whether the risk screen runs. Mirrors the repo's
// feature-flag precedent: default paths stay model-free.
func Enabled() bool {
	return os.Getenv("SETTLE_JEV") == "1"
}

// Threshold is the risk score at or above which an ALLOW escalates.
func Threshold() int {
	if v, err := strconv.Atoi(os.Getenv("SETTLE_JEV_THRESHOLD")); err == nil && v >= 0 && v <= 100 {
		return v
	}
	return 70
}

func clampArgs(args map[string]any) map[string]any {
	out := map[string]any{}
	total := 0
	for k, v := range args {
		s := fmt.Sprintf("%v", v)
		r := []rune(s)
		if len(r) > 200 {
			s = string(r[:200])
		}
		out[k] = s
		total += len(s) + len(k)
		if total >= maxArgsChars {
			break
		}
	}
	return out
}

// ScreenRisk scores one proposed action. Errors when the judgment could
// not be obtained — the caller escalates on any error (fail closed).
func ScreenRisk(a Action) (Risk, error) {
	key := os.Getenv("JEV_API_KEY")
	if key == "" {
		return Risk{}, fmt.Errorf("jev: JEV_API_KEY is not set")
	}
	endpoint := os.Getenv("JEV_API_URL")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	body, err := json.Marshal(map[string]any{
		"model": "jev-latest",
		"state": map[string]any{
			"tool":           a.Tool,
			"action":         a.Name,
			"amount_cents":   a.AmountCents,
			"arguments":      clampArgs(a.Arguments),
			"classes":        a.Classes,
			"agent":          map[string]any{"name": a.AgentName, "groups": a.AgentGroups, "environment": a.AgentEnv},
			"sibling_count":  a.SiblingCount,
			"sibling_total":  a.SiblingTotal,
			"risk_threshold": Threshold(),
		},
		"questions": map[string]any{
			"risk": map[string]any{
				"type":         "score",
				"instructions": "How risky is this proposed agent action? Weigh the amount, the arguments, the agent's role, and the transaction siblings (repeated similar actions in one transaction raise risk).",
				"criteria":     levels,
			},
		},
	})
	if err != nil {
		return Risk{}, fmt.Errorf("jev: encode request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Risk{}, fmt.Errorf("jev: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return Risk{}, fmt.Errorf("jev: request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Risk{}, fmt.Errorf("jev: endpoint returned %s", res.Status)
	}
	var decoded systemOneResponse
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return Risk{}, fmt.Errorf("jev: decode response: %w", err)
	}
	risk, ok := decoded.Answers["risk"]
	if !ok {
		return Risk{}, fmt.Errorf("jev: response missing risk")
	}
	score := int(risk.Score*25 + 0.5)
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return Risk{Score100: score, Confidence: risk.Confidence}, nil
}
