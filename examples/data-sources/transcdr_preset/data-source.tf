# A system preset, looked up by slug. (Your own presets can be looked up by
# slug or by id.)
data "transcdr_preset" "hls" {
  slug = "hls-av1-abr"
}

# Start from it and change one field: merge() replaces `codec` and keeps the
# rest of the system preset's specification.
resource "transcdr_preset" "hls_h264" {
  name   = "HLS H.264"
  output = jsonencode(merge(jsondecode(data.transcdr_preset.hls.output), { codec = "h264" }))
}
