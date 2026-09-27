# A narrowly scoped key for a CI job that submits and watches jobs.
resource "transcdr_api_key" "ci" {
  name   = "CI"
  scopes = ["jobs:write", "jobs:read", "presets:read"]
  mode   = "live"
}

output "ci_key" {
  value     = transcdr_api_key.ci.secret
  sensitive = true
}
