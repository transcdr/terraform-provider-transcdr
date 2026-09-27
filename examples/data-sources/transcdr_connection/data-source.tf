# A connection made outside this configuration (in the dashboard, or by
# another team's Terraform), looked up by its exact name. Use `id` instead
# when names are not unique. Secrets are never returned.
data "transcdr_connection" "ingest" {
  name = "Ingest bucket"
}

# Watch it without managing it here.
resource "transcdr_automation" "watch" {
  name   = "Watch ingest"
  source = { connection_id = data.transcdr_connection.ingest.id }
  preset = "hls-av1-abr"
}
