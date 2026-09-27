# Constant bit rate HLS: each rung at its own rate, or quality.bitrate, held
# within a 1.5 s buffer. Write only the fields you want: the rest come from
# the defaults, and the full result is in resolved_output.
resource "transcdr_preset" "cbr_broadcast" {
  name        = "Broadcast CBR 1080p"
  slug        = "broadcast-cbr-1080p" # optional; made from the name when omitted
  description = "H.264 HLS at constant bit rates."
  output = jsonencode({
    mode    = "hls"
    codec   = "h264"
    quality = { target = "cbr", bitrate = "3M", buffer_ms = 1500 }
    renditions = [
      { width = 1920, height = 1080, bitrate = "6M" },
      { width = 1280, height = 720 }, # takes quality.bitrate
    ]
  })
}

# An automatic ABR ladder up to 1080p, coded to a quality level rather than a
# rate. Every change updates the preset in place; a field you remove goes back
# to its default.
resource "transcdr_preset" "av1_ladder" {
  name = "AV1 ladder"
  output = jsonencode({
    mode    = "hls"
    codec   = "av1"
    ladder  = { max_short_side = 1080 }
    quality = { target = "high" } # visually_lossless | high | standard | low | vmaf=N
  })
  metadata = { team = "web" }
}
