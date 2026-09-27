data "transcdr_organization" "this" {}

# A system preset to start from.
data "transcdr_preset" "hls_h264" {
  slug = "hls-h264-abr"
}

# Constant bit rate HLS for a broadcast chain: every rung at a fixed rate
# within a 1.5 s buffer. A rung without its own bitrate uses quality.bitrate.
resource "transcdr_preset" "cbr_broadcast" {
  name        = "Broadcast CBR 1080p"
  slug        = "broadcast-cbr-1080p"
  description = "H.264 HLS at constant bit rates, for players and networks that need predictable bandwidth."
  output = jsonencode({
    mode            = "hls"
    codec           = "h264"
    segment_seconds = 4
    quality = {
      target    = "cbr"
      bitrate   = "3M"
      buffer_ms = 1500
    }
    renditions = [
      { width = 1920, height = 1080, bitrate = "6M" },
      { width = 1280, height = 720 },
      { width = 854, height = 480, bitrate = "1200k" },
    ]
    audio = { mode = "auto", bitrate = "128k" }
  })
  metadata = {
    team = "broadcast"
  }
}

# The system preset, in H.265: every other field is the system preset's.
resource "transcdr_preset" "web_h265_abr" {
  name   = "Web H.265 ABR"
  output = jsonencode(merge(jsondecode(data.transcdr_preset.hls_h264.output), { codec = "h265" }))
}

# A quality-targeted AV1 preset for archives.
resource "transcdr_preset" "archive" {
  name = "Archive AV1"
  output = jsonencode({
    mode      = "single"
    codec     = "av1"
    quality   = { target = "vmaf=95" }
    bit_depth = "10bit"
    color     = "passthrough"
  })
}

# Completed and failed jobs, POSTed and signed to your server.
resource "transcdr_event_destination" "https" {
  url         = var.webhook_url
  description = "Job outcomes"
  events      = ["job.completed", "job.failed", "job.delivery_failed", "connection.disabled"]

  # Change to rotate signing_secret.
  secret_version = var.secret_version
}
