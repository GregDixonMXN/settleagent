# Production deploy (DigitalOcean, via OpenTofu)

Solo-founder path: App Platform builds both containers from GitHub,
Managed Postgres holds state. ~$40-60/mo at v0.1 sizes.

## First deploy

1. `tofu init`
2. Create `prod.tfvars`:
   do_token, repo_url, dashboard_password (long random string).
   Leave dashboard_api_url empty for now.
3. `tofu apply -var-file=prod.tfvars`
   Note the api_url output.
4. Second pass: set dashboard_api_url to the api_url, re-apply so the
   dashboard builds against the API.
5. Mint the dashboard operator token: call the live API's boot log
   (App Platform runtime logs print it once on first boot), then set
   NEXT_PUBLIC_API_TOKEN in the App spec and redeploy. Rotate by
   re-minting; tokens are bcrypt-hashed server-side.

## Notes

- State is local by default; move to remote state before adding operators.
- No Jaeger in prod yet — set OTEL_EXPORTER_OTLP_ENDPOINT when you run a
  collector. Trace IDs still generate without one.
- Single DB node: fine for v0.1; add read replicas when measured load says so.
