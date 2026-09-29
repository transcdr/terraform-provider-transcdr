# =============================================================================
# Presets and an HTTPS event destination
# =============================================================================
#
# A preset is a named output specification: what codec, which renditions, at
# what quality or rate, HLS or plain MP4. Jobs and automations refer to it by
# id (pre_…) or by slug, instead of repeating the settings every time.
#
# How a preset's `output` works in Terraform:
#
#   * You write only the fields you want, as JSON (jsonencode is the easiest
#     way). Transcdr fills in every other field from its defaults; the full
#     result is in the preset's `resolved_output` attribute.
#   * Terraform compares only the fields you wrote. Defaults filled in by the
#     API never show up as changes, but a field someone edits in the
#     dashboard does.
#   * Every change updates the preset in place (same id). Terraform sends the
#     whole preset, so a field you remove goes back to its default.
#
# The specification's fields: mode (single | hls), codec (av1 | h264 | h265),
# renditions [{width, height, bitrate?, label?}] or ladder {max_short_side},
# quality {target, crf, bitrate, buffer_ms}, gop, segment_seconds (hls),
# audio {mode, bitrate}, subtitles, color, bit_depth, max_fps, filters, trim.

# -----------------------------------------------------------------------------
# Lookups
# -----------------------------------------------------------------------------

# The organization the API key belongs to: its plan and the plan's limits.
# Used in outputs.tf; handy for guarding a config, e.g. checking that the plan
# allows 2160p before creating a 4K preset.
data "transcdr_organization" "this" {}

# A system preset, to build on. System presets have their slug as their id:
# web-av1-1080p, web-av1-720p, hls-av1-abr, hls-h264-abr,
# mp4-h264-compat-1080p, mp4-h265-1080p, hdr10-av1-2160p,
# social-vertical-1080x1920, audio-strip-av1-720p, archive-av1-high.
data "transcdr_preset" "hls_h264" {
  slug = "hls-h264-abr"
}

# -----------------------------------------------------------------------------
# A constant bit rate preset
# -----------------------------------------------------------------------------

# Constant bit rate (CBR) codes every rendition at a fixed rate instead of to
# a quality level, for players, networks and broadcast chains that need
# predictable bandwidth. Each rendition uses its own `bitrate`, else
# quality.bitrate, else a default for its resolution and codec. HLS
# BANDWIDTH then states each rung's actual rate.
resource "transcdr_preset" "cbr_broadcast" {
  name = "Broadcast CBR 1080p"

  # Optional: the slug is made from the name when you leave it out. It must
  # be unique in the organization and not a system preset's.
  slug = "broadcast-cbr-1080p"

  description = "H.264 HLS at constant bit rates, for players and networks that need predictable bandwidth."

  # The whole output specification (output spec v2): every field its kind
  # needs, nothing left to a default. A missing field is reported at plan
  # time, every one at once.
  output = jsonencode({
    kind = "video"

    # A CMAF/HLS package with a master playlist; segments 1 to 20 seconds.
    container = { format = "hls", segment_seconds = 4 }

    video = {
      codec = "h264"
      cbr = {
        bitrate   = "3M" # for sizes without their own rate; or "standard"
        buffer_ms = 1500 # the rate buffer, 100 to 10000 ms
      }
      bit_depth  = "8bit"
      color      = "sdr"
      frame_rate = { max = "source" } # or a cap such as 30
      gop        = "segment"          # one keyframe per segment
      filters    = []
    }

    audio = {
      handling        = "encode"
      codec           = "aac" # AAC-LC plays everywhere
      bitrate         = "128k"
      channels        = "source"
      he_aac          = "auto"
      stereo_fallback = false
    }

    # Rates are 100k to 200M. Width and height are a maximum box, even.
    renditions = {
      sizes = [
        { label = "by_size", width = 1920, height = 1080, fit = "contain", orientation = "auto", upscale = false, video = { cbr = { bitrate = "6M" } } },
        { label = "by_size", width = 1280, height = 720, fit = "contain", orientation = "auto", upscale = false }, # takes video.cbr.bitrate: 3M
        { label = "by_size", width = 854, height = 480, fit = "contain", orientation = "auto", upscale = false, video = { cbr = { bitrate = "1200k" } } },
      ]
    }

    subtitles = { tracks = "all" }
    trim      = { start = 0, end = "source" }
    privacy   = { preset = "strip_all" } # no location, device, capture time or tags
  })

  # Up to 20 string keys; not used by Transcdr, only stored and returned.
  metadata = {
    team = "broadcast"
  }
}

# -----------------------------------------------------------------------------
# A preset built on a system preset
# -----------------------------------------------------------------------------

# Take the system preset's whole specification and change one field. The spec
# is in sections, so the codec is replaced inside `video`; everything else
# (its ladder, segment length, audio) is the system preset's. A new version of
# that system preset shows up as a difference in the next plan.
locals {
  hls_h264 = jsondecode(data.transcdr_preset.hls_h264.output)
}

resource "transcdr_preset" "web_h265_abr" {
  name   = "Web H.265 ABR"
  output = jsonencode(merge(local.hls_h264, { video = merge(local.hls_h264.video, { codec = "h265" }) }))
}

# -----------------------------------------------------------------------------
# A quality-targeted preset
# -----------------------------------------------------------------------------

# For archives: one MP4 at the source's size, coded to a perceptual quality
# score instead of a rate. vmaf=95 is close to visually lossless; lower
# numbers make smaller files.
resource "transcdr_preset" "archive" {
  name = "Archive AV1"
  output = jsonencode({
    kind      = "video"
    container = { format = "mp4" } # one faststart MP4 per size
    video = {
      codec      = "av1"
      quality    = "vmaf=95"
      bit_depth  = "10bit"
      color      = "passthrough" # keep the source's colour (SDR or HDR) as it is
      frame_rate = { max = "source" }
      gop        = { seconds = 2 }
      filters    = []
    }
    audio      = { handling = "encode", codec = "flac", channels = "source", he_aac = "auto", bit_depth = "source", flac_compression = "best" }
    renditions = { source_size = { label = "by_size", fit = "contain", upscale = false } }
    subtitles  = { tracks = "all" }
    trim       = { start = 0, end = "source" }
    privacy    = { preset = "keep_all" } # an archive keeps where, when and on what it was made
  })
}

# -----------------------------------------------------------------------------
# An HTTPS event destination
# -----------------------------------------------------------------------------

# Transcdr POSTs each event to var.webhook_url as JSON, with a
# Transcdr-Signature header:
#
#   t=<unix time>,v1=<hex HMAC-SHA256(signing_secret, "<t>.<raw body>")>
#
# Check it before trusting the body (every Transcdr SDK has a
# verifySignature / constructEvent helper), and reject timestamps more than a
# few minutes old.
resource "transcdr_event_destination" "https" {
  url         = var.webhook_url
  description = "Job outcomes"

  # The event types to send; leave `events` out (or use ["*"]) for all of
  # them. The full list is in the transcdr_event_destination docs.
  events = [
    "job.completed",       # outputs are ready
    "job.failed",          # the job gave up; `error` says why
    "job.delivery_failed", # outputs could not be delivered to a connection
    "connection.disabled", # a connection stopped working and was turned off
  ]

  # Change var.secret_version to rotate signing_secret.
  secret_version = var.secret_version
}
