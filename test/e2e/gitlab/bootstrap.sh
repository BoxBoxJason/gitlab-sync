#!/usr/bin/env bash
# Creates an admin personal access token on the running e2e GitLab and prints it on stdout.
set -euo pipefail

compose_file="$(dirname "$0")/compose.yaml"
token="$(openssl rand -hex 16)"

docker compose -f "$compose_file" exec -T gitlab gitlab-rails runner "
  user = User.find_by_username('root')
  pat = user.personal_access_tokens.create!(
    name: 'e2e', scopes: %w[api read_repository write_repository], expires_at: 7.days.from_now)
  pat.set_token('${token}')
  pat.save!
" >&2

# No rate limiting (the suite hammers the API from one client), no outbound calls
curl -fsS -o /dev/null -X PUT -H "PRIVATE-TOKEN: ${token}" "${GITLAB_URL:-http://localhost:8181}/api/v4/application/settings" \
  -d throttle_authenticated_api_enabled=false -d throttle_authenticated_web_enabled=false \
  -d throttle_unauthenticated_api_enabled=false -d throttle_unauthenticated_web_enabled=false \
  -d version_check_enabled=false -d usage_ping_enabled=false -d gravatar_enabled=false \
  -d signup_enabled=false -d auto_devops_enabled=false

echo "$token"
