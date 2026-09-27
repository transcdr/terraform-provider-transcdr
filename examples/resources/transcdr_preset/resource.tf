# Constant bit rate HLS: each rung at its own rate, or quality.bitrate.
resource "transcdr_preset" "cbr_broadcast" {
  name        = "Broadcast CBR 1080p"
  slug        = "broadcast-cbr-1080p"
  description = "H.264 HLS at constant bit rates."
  output = jsonencode({
    mode    = "hls"
    codec   = "h264"
    quality = { target = "cbr", bitrate = "3M", buffer_ms = 1500 }
    renditions = [
      { width = 1920, height = 1080, bitrate = "6M" },
      { width = 1280, height = 720 },
    ]
  })
}

# An automatic ladder at a quality level.
resource "transcdr_preset" "av1_ladder" {
  name = "AV1 ladder"
  output = jsonencode({
    mode    = "hls"
    codec   = "av1"
    ladder  = { max_short_side = 1080 }
    quality = { target = "high" }
  })
  metadata = { team = "web" }
}
