# SettleAgent Pilot — $500/mo, capped cohorts

For teams whose agents already touch money, APIs, or customer data and
currently run on vibes. You bring one real agent workflow. Within two
weeks it runs governed: every consequential action passes identity,
policy, approval, and leaves a tamper-evident receipt.

## What you need

- One agent workflow with real side effects (refunds, issues, deploys,
  emails — Stripe test-mode works for evaluation, then your keys).
- One human approver with 15 minutes a day during onboarding.
- A machine or VPC to self-host on (compose file provided), or Docker.

## Week 1 — first governed action

1. Clone, `docker compose up`, run the mock demo (5 minutes).
2. We map your workflow to policy rules together: what is auto-allowed,
   what needs approval, what is denied. Deny-by-default.
3. Your agent calls SettleAgent with its own API key. First live action
   executes under policy with receipts.

## Week 2 — production posture

4. Real credentials sealed at rest, Postgres backend, your domain
   allowlists, approver workflow in the dashboard.
5. Audit export review: every action explainable to your security person.
6. Go/no-go. Stay on Pilot month-to-month, or leave with your data
   (Postgres is yours, receipts verify offline).

## What Pilot is not

- No SSO/SAML, no SIEM export, no SLA — that is Enterprise.
- No hosted option — you self-host; we help. BSL-1.1: free to run for
  your own agents, contact us to offer it as a service.
- Approval notifications today mean watching the dashboard queue.
  Webhook/email pings are on the near-term roadmap — tell us which
  channel and it moves up.

## Start

Open an issue titled `Pilot:` with: what your agents spend and touch,
roughly how many actions a day, and who approves. If it is a fit, first
governed action live within two weeks.
