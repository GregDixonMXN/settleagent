package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/GregDixonMXN/settleagent/internal/actions"
	"github.com/GregDixonMXN/settleagent/internal/domain"
)

// GitHub client (personal access token, per-org credential "github").
// Reads are READ_ONLY; writes are external communication or privileged;
// merges are never silent.
func githubHandler(getCred func(orgID string) (string, bool), method, pathTmpl string) actions.ToolHandler {
	return func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
		token, ok := getCred(a.OrgID)
		if !ok {
			return nil, fmt.Errorf("github integration not configured for this org")
		}
		owner, ok := strArg(a.Arguments, "owner")
		if !ok {
			return nil, fmt.Errorf("github action needs an owner")
		}
		repo, ok := strArg(a.Arguments, "repo")
		if !ok {
			return nil, fmt.Errorf("github action needs a repo")
		}
		path := strings.ReplaceAll(strings.ReplaceAll(pathTmpl, "{owner}", urlEncode(owner)), "{repo}", urlEncode(repo))
		if n, ok := strArg(a.Arguments, "number", "issue", "pr"); ok {
			path = strings.ReplaceAll(path, "{number}", urlEncode(n))
		}
		headers := map[string]string{
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": "2022-11-28",
		}
		var out map[string]any
		var err error
		if method == "GET" {
			out, _, err = doJSON(ctx, "GET", githubBaseURL+path, token, "", nil, headers)
		} else {
			body, _ := json.Marshal(a.Arguments)
			out, _, err = doJSONRaw(ctx, method, githubBaseURL+path, token, body, headers)
		}
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

func githubClasses() map[string][]domain.ActionClass {
	ro := []domain.ActionClass{domain.ClassReadOnly}
	ext := []domain.ActionClass{domain.ClassExternalCommunication}
	priv := []domain.ActionClass{domain.ClassPrivileged, domain.ClassExternalCommunication}
	return map[string][]domain.ActionClass{
		"read_repo":    ro,
		"read_issue":   ro,
		"create_issue": ext,
		"comment":      ext,
		"open_pr":      ext,
		"merge_pr":     priv,
	}
}
