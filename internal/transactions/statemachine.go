package transactions

import (
	"fmt"

	"github.com/agentguard/agentguard/internal/domain"
)

var allowed = map[domain.TxnStatus][]domain.TxnStatus{
	domain.TxnCreated:          {domain.TxnPlanning, domain.TxnAborting, domain.TxnFailed},
	domain.TxnPlanning:         {domain.TxnExecuting, domain.TxnAwaitingApproval, domain.TxnAborting, domain.TxnFailed},
	domain.TxnAwaitingApproval: {domain.TxnExecuting, domain.TxnAborting, domain.TxnFailed},
	domain.TxnExecuting:        {domain.TxnCommitted, domain.TxnAwaitingApproval, domain.TxnAborting, domain.TxnCompensating, domain.TxnFailed},
	domain.TxnAborting:         {domain.TxnCompensating, domain.TxnRolledBack, domain.TxnFailed},
	domain.TxnCompensating:     {domain.TxnRolledBack, domain.TxnPartiallyCompensated, domain.TxnFailed},
}

func CanTransition(from, to domain.TxnStatus) bool {
	if from == to {
		return true
	}
	// Terminal states never leave.
	switch from {
	case domain.TxnCommitted, domain.TxnRolledBack, domain.TxnPartiallyCompensated, domain.TxnFailed:
		return false
	}
	for _, s := range allowed[from] {
		if s == to {
			return true
		}
	}
	return false
}

func MustTransition(t *domain.Transaction, to domain.TxnStatus) error {
	if !CanTransition(t.Status, to) {
		return fmt.Errorf("illegal transaction transition %s -> %s", t.Status, to)
	}
	t.Status = to
	return nil
}

// ReorderForSafety moves reversible/compensable actions before irreversible
// ones only when the caller marks the sequence as reorderable. MVP default:
// stable deterministic order by seq; planner hook preserved for later.
func OrderActions(actions []domain.TxnAction, reorderable bool) []domain.TxnAction {
	if !reorderable {
		return actions
	}
	safe := func(a domain.TxnAction) bool {
		for _, c := range a.Classes {
			if c == domain.ClassIrreversible || c == domain.ClassDestructive {
				return false
			}
		}
		return true
	}
	var s, r []domain.TxnAction
	for _, a := range actions {
		if safe(a) {
			s = append(s, a)
		} else {
			r = append(r, a)
		}
	}
	return append(s, r...)
}
