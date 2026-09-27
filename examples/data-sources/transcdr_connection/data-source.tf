# A connection made in the dashboard.
data "transcdr_connection" "ingest" {
  name = "Ingest bucket"
}

resource "transcdr_automation" "watch" {
  name   = "Watch ingest"
  source = { connection_id = data.transcdr_connection.ingest.id }
  preset = "hls-av1-abr"
}
