# A system preset, looked up by slug: its latest version. (Your own presets
# can be looked up by slug or by id.)
data "transcdr_preset" "hls" {
  slug = "hls-av1-abr"
}

# A version of it: versions never change.
data "transcdr_preset" "hls_v1" {
  slug    = "hls-av1-abr"
  version = 1
}

# Start from its whole specification and change one field. The spec is in
# sections, so the codec is replaced inside `video`, keeping the rest.
locals {
  hls = jsondecode(data.transcdr_preset.hls_v1.output)
}

resource "transcdr_preset" "hls_h264" {
  name   = "HLS H.264"
  output = jsonencode(merge(local.hls, { video = merge(local.hls.video, { codec = "h264" }) }))
}
