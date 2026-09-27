data "transcdr_preset" "hls" {
  slug = "hls-av1-abr"
}

# Start from a system preset and change one field.
resource "transcdr_preset" "hls_h264" {
  name   = "HLS H.264"
  output = jsonencode(merge(jsondecode(data.transcdr_preset.hls.output), { codec = "h264" }))
}
