# Constant bit rate HLS: each size at its own rate, or video.cbr.bitrate,
# held within a 1.5 s buffer. A preset's output is the whole specification
# (output spec v2): every field its kind needs, nothing left to a default.
# A missing field is reported at plan time; resolved_output is what the API
# stored.
resource "transcdr_preset" "cbr_broadcast" {
  name        = "Broadcast CBR 1080p"
  slug        = "broadcast-cbr-1080p" # optional; made from the name when omitted
  description = "H.264 HLS at constant bit rates."
  output = jsonencode({
    kind      = "video"
    container = { format = "hls", segment_seconds = 6 }
    video = {
      codec      = "h264"
      cbr        = { bitrate = "3M", buffer_ms = 1500 } # or quality = "high", or crf = 23
      bit_depth  = "8bit"
      color      = "sdr"
      frame_rate = { max = "source" }
      gop        = "segment" # one keyframe per segment; or { seconds = 2 }
      filters    = []
    }
    audio = {
      handling        = "encode"
      codec           = "aac"
      bitrate         = "standard" # by codec and layout: 128k for stereo AAC
      channels        = "source"
      he_aac          = "auto"
      stereo_fallback = false
    }
    renditions = {
      sizes = [
        { label = "by_size", width = 1920, height = 1080, fit = "contain", orientation = "auto", upscale = false, video = { cbr = { bitrate = "6M" } } },
        { label = "by_size", width = 1280, height = 720, fit = "contain", orientation = "auto", upscale = false }, # takes video.cbr.bitrate
      ]
    }
    subtitles = { tracks = "all" }
    trim      = { start = 0, end = "source" }
    privacy   = { preset = "strip_all" }
  })
}

# An automatic ABR ladder up to 1080p, coded to a quality level rather than a
# rate. Every change updates the preset in place, as a new version.
resource "transcdr_preset" "av1_ladder" {
  name = "AV1 ladder"
  output = jsonencode({
    kind      = "video"
    container = { format = "hls", segment_seconds = 4 }
    video = {
      codec      = "av1"
      quality    = "high" # visually_lossless | high | standard | low | vmaf=N
      bit_depth  = "from_color"
      color      = "sdr"
      frame_rate = { max = "source" }
      gop        = "segment"
      filters    = []
    }
    audio      = { handling = "auto", codec = "opus", bitrate = "standard", channels = "source", he_aac = "auto", stereo_fallback = false }
    renditions = { ladder = { max_short_side = 1080, fit = "contain", upscale = false } }
    subtitles  = { tracks = "all" }
    trim       = { start = 0, end = "source" }
    privacy    = { preset = "strip_all" }
  })
  metadata = { team = "web" }
}

# Stills from a video: 12 evenly spaced JPEG frames.
resource "transcdr_preset" "contact_sheet" {
  name = "Contact sheet"
  output = jsonencode({
    kind       = "image"
    image      = { formats = ["jpeg"], quality = { jpeg = 80 }, color_profile = "srgb", frames = { count = 12 } }
    renditions = { sizes = [{ label = "sheet", width = 320, height = 320, fit = "contain", orientation = "auto", upscale = false }] }
    privacy    = { preset = "strip_all" }
  })
}

# Pin the version a job or automation uses: "<slug>@<version>".
output "cbr_broadcast_pinned" {
  value = "${transcdr_preset.cbr_broadcast.slug}@${transcdr_preset.cbr_broadcast.version}"
}
