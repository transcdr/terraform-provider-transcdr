# A narrowly scoped key for a CI job that submits and watches jobs. The key
# managing it must hold these scopes itself. Keys cannot be changed: any
# change here creates a new key (with a new secret) and revokes the old one.
resource "transcdr_api_key" "ci" {
  name   = "CI"
  scopes = ["jobs:write", "jobs:read", "presets:read"]
  mode   = "live" # or "test": jobs complete with placeholder outputs, free

  # Optional: the key stops working at this time.
  # expires_at = "2027-01-01T00:00:00Z"
}

# The secret exists only in the apply that created the key, and is kept in
# the Terraform state: keep the state private. Pass it on, e.g. to your CI
# system's secret store, rather than printing it.
output "ci_key" {
  value     = transcdr_api_key.ci.secret
  sensitive = true
}
