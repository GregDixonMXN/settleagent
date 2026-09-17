package jev

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func stubServer(t *testing.T, score, confidence float64, check func(t *testing.T, body map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if check != nil {
			check(t, body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{
				"risk": map[string]any{"score": score, "confidence": confidence},
			},
		})
	}))
}

func TestScreenRiskMapsScore(t *testing.T) {
	srv := stubServer(t, 0.84, 0.9, func(t *testing.T, body map[string]any) {
		q := body["questions"].(map[string]any)["risk"].(map[string]any)
		if q["type"] != "score" {
			t.Errorf("risk type = %v", q["type"])
		}
		if len(q["criteria"].([]any)) != 5 {
			t.Errorf("want 5 risk levels, got %v", q["criteria"])
		}
		state := body["state"].(map[string]any)
		if state["tool"] != "stripe" || state["sibling_count"] != float64(3) {
			t.Errorf("state not passed through: %v", state)
		}
	})
	defer srv.Close()
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)
	r, err := ScreenRisk(Action{Tool: "stripe", Name: "refund", AmountCents: 8000, SiblingCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.Score100 != 21 || r.Confidence != 0.9 {
		t.Errorf("risk = %+v", r)
	}
}

func TestScreenRiskTruncatesArguments(t *testing.T) {
	srv := stubServer(t, 0, 1, func(t *testing.T, body map[string]any) {
		for _, v := range body["state"].(map[string]any)["arguments"].(map[string]any) {
			if len([]rune(v.(string))) > 200 {
				t.Errorf("argument not clamped: %d chars", len([]rune(v.(string))))
			}
		}
	})
	defer srv.Close()
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)
	a := Action{Arguments: map[string]any{"blob": strings.Repeat("x", 5000)}}
	if _, err := ScreenRisk(a); err != nil {
		t.Fatal(err)
	}
}

func TestScreenRiskRequiresKey(t *testing.T) {
	old, had := os.LookupEnv("JEV_API_KEY")
	_ = os.Unsetenv("JEV_API_KEY")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("JEV_API_KEY", old)
		}
	})
	if _, err := ScreenRisk(Action{}); err == nil {
		t.Error("expected error with no key")
	}
}

func TestThresholdDefaultsAndParses(t *testing.T) {
	t.Setenv("SETTLE_JEV_THRESHOLD", "")
	if Threshold() != 70 {
		t.Errorf("default = %d", Threshold())
	}
	t.Setenv("SETTLE_JEV_THRESHOLD", "50")
	if Threshold() != 50 {
		t.Errorf("parsed = %d", Threshold())
	}
	t.Setenv("SETTLE_JEV_THRESHOLD", "junk")
	if Threshold() != 70 {
		t.Errorf("junk = %d", Threshold())
	}
}
