# The organization the API key belongs to, with its plan's limits and
# features.
data "transcdr_organization" "this" {}

# Connections and automations need the plan's "integrations" feature; event
# destinations need "webhooks". A precondition turns a confusing API error
# into a clear message at plan time.
resource "transcdr_connection" "ingest" {
  name = "Ingest bucket"
  kind = "s3"
  config = {
    bucket = "media-in"
    region = "us-east-1"
  }

  lifecycle {
    precondition {
      condition     = contains(data.transcdr_organization.this.features, "integrations")
      error_message = "Connections need the Starter plan or above; this organization is on ${data.transcdr_organization.this.plan_name}."
    }
  }
}
