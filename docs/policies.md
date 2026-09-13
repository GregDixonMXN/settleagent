# Policies — Declarative JSON Model v1

Policies are versioned JSON documents. Rules evaluate **in order, first match wins**. A request that matches no rule gets `REQUIRE_APPROVAL` (default-deny-leaning; never implicit `ALLOW`).

## Rule schema

```json
{
  "model": "v1",
  "rules": [
    {
      "id": "support-refund-small",
      "match": {
        "agent_group": "support",
        "tool": "billing",
        "action": "refund",
        "amount_lte": 50,
        "recipients": []
      },
      "effect": "ALLOW",
      "explanation": "Support can auto-refund up to $50."
    }
  ]
}
```

Match fields (all optional; absent = wildcard): `agent_group`, `tool`, `action`, `amount_lte`, `amount_gt`, `recipients` (all must match for recipient-scoped rules; empty = any).
`effect`: `ALLOW` | `DENY` | `REQUIRE_APPROVAL` | `ALLOW_WITH_CONSTRAINTS` (+ optional `constraints` object).
`explanation`: human-readable string, surfaced in the error `why` field and audit log. Required on every rule.

## Example ruleset (8 rules)

```json
{
  "model": "v1",
  "rules": [
    {
      "id": "support-refund-small",
      "match": { "agent_group": "support", "tool": "billing", "action": "refund", "amount_lte": 50 },
      "effect": "ALLOW",
      "explanation": "Support can auto-refund up to $50."
    },
    {
      "id": "support-refund-large",
      "match": { "agent_group": "support", "tool": "billing", "action": "refund", "amount_gt": 50 },
      "effect": "REQUIRE_APPROVAL",
      "explanation": "Refunds over $50 need a lead's approval."
    },
    {
      "id": "prod-db-delete",
      "match": { "tool": "postgres", "action": "delete", "recipients": ["prod"] },
      "effect": "REQUIRE_APPROVAL",
      "explanation": "Deletes against the production database need human approval."
    },
    {
      "id": "email-bulk",
      "match": { "tool": "email", "action": "send", "recipients_gt": 50 },
      "effect": "REQUIRE_APPROVAL",
      "explanation": "Bulk email to more than 50 recipients needs approval."
    },
    {
      "id": "no-iam",
      "match": { "tool": "iam" },
      "effect": "DENY",
      "explanation": "IAM changes are forbidden for agents."
    },
    {
      "id": "staging-deploy",
      "match": { "tool": "deploy", "recipients": ["staging"] },
      "effect": "ALLOW",
      "explanation": "Deploys to staging run automatically."
    },
    {
      "id": "prod-deploy",
      "match": { "tool": "deploy", "recipients": ["prod"] },
      "effect": "REQUIRE_APPROVAL",
      "explanation": "Production deploys need release-manager approval."
    },
    {
      "id": "default",
      "match": {},
      "effect": "REQUIRE_APPROVAL",
      "explanation": "Anything not explicitly allowed needs approval."
    }
  ]
}
```

Notes:
- Rules 1–2 implement the **support refund tiers** (≤$50 auto, >$50 approval).
- Rule 4 uses recipient-count matching for the **email > 50** case (`recipients_gt`; equivalent: `amount_gt` on recipient count — implementations must document which they use).
- Rule 5 (**no IAM**) is a blanket `DENY` on the `iam` tool, placed before looser rules so it always wins.
- Rule 8 is the catch-all; it guarantees no implicit `ALLOW`.
