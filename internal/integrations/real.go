package integrations

import (
	"log"
	"os"

	"github.com/GregDixonMXN/settleagent/internal/actions"
	"github.com/GregDixonMXN/settleagent/internal/domain"
	"github.com/GregDixonMXN/settleagent/internal/store"
)

// CredentialNames are the per-org third-party credentials.
var CredentialNames = map[string]bool{"stripe": true, "github": true, "postgres": true}

// RegisterReal replaces mock handlers with live integrations that read
// per-org credentials. Missing credentials fail loudly at execution time
// ("integration not configured") — never silent mock fallback, except when
// AG_DEMO_MOCKS=1 keeps the local demo running on mocks (loudly logged).
func RegisterReal(reg *actions.Registry, st store.Store) {
	if os.Getenv("AG_DEMO_MOCKS") == "1" {
		log.Print("integrations: AG_DEMO_MOCKS=1 — mock handlers active (local demo only, never production)")
		return
	}
	cred := func(name string) func(string) (string, bool) {
		return func(orgID string) (string, bool) {
			return st.GetIntegrationCredential(orgID, name)
		}
	}
	reg.Register("stripe", "refund",
		[]domain.ActionClass{domain.ClassFinancial, domain.ClassCompensable},
		stripeRefundHandler(cred("stripe")), nil)
	reg.Register("stripe", "read_customer",
		[]domain.ActionClass{domain.ClassReadOnly},
		stripeReadCustomerHandler(cred("stripe")), nil)
	reg.RegisterReconciler("stripe", "refund", stripeRefundReconciler(cred("stripe")))

	gh := githubClasses()
	reg.Register("github", "read_repo", gh["read_repo"],
		githubHandler(cred("github"), "GET", "/repos/{owner}/{repo}"), nil)
	reg.Register("github", "read_issue", gh["read_issue"],
		githubHandler(cred("github"), "GET", "/repos/{owner}/{repo}/issues/{number}"), nil)
	reg.Register("github", "create_issue", gh["create_issue"],
		githubHandler(cred("github"), "POST", "/repos/{owner}/{repo}/issues"), nil)
	reg.Register("github", "comment", gh["comment"],
		githubHandler(cred("github"), "POST", "/repos/{owner}/{repo}/issues/{number}/comments"), nil)
	reg.Register("github", "open_pr", gh["open_pr"],
		githubHandler(cred("github"), "POST", "/repos/{owner}/{repo}/pulls"), nil)
	reg.Register("github", "merge_pr", gh["merge_pr"],
		githubHandler(cred("github"), "PUT", "/repos/{owner}/{repo}/pulls/{number}/merge"), nil)

	reg.Register("postgres", "query", postgresClasses()["query"],
		postgresQueryHandler(cred("postgres")), nil)
	reg.Register("http", "request", httpClasses()["request"],
		httpRequestHandler(st), nil)
}
