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
#   * Changing a value updates the preset in place (same id).
#   * Removing a field you had written replaces the preset (a new id; the slug
#     carries over), because the API merges updates into the stored
#     specification and would otherwise keep the old value.
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

  output = jsonencode({
    mode            = "hls" # a CMAF/HLS package with a master playlist
    codec           = "h264"
    segment_seconds = 4 # HLS segment length, 1 to 20

    quality = {
      target    = "cbr"
      bitrate   = "3M" # for renditions without their own bitrate
      buffer_ms = 1500 # the rate buffer, 100 to 10000 ms (default 1000)
    }

    # Rates are 100k to 200M. Width and height must be even.
    renditions = [
      { width = 1920, height = 1080, bitrate = "6M" },
      { width = 1280, height = 720 }, # takes quality.bitrate: 3M
      { width = 854, height = 480, bitrate = "1200k" },
    ]

    audio = { mode = "auto", bitrate = "128k" }
  })

  # Up to 20 string keys; not used by Transcdr, only stored and returned.
  metadata = {
    team = "broadcast"
  }
}

# -----------------------------------------------------------------------------
# A preset built on a system preset
# -----------------------------------------------------------------------------

# Take the system preset's full specification and change one field. merge()
# overwrites `codec`; everything else (its ladder, segment length, audio) is
# the system preset's. If Transcdr ever changes that system preset, the next
# plan shows the difference here.
resource "transcdr_preset" "web_h265_abr" {
  name   = "Web H.265 ABR"
  output = jsonencode(merge(jsondecode(data.transcdr_preset.hls_h264.output), { codec = "h265" }))
}

# -----------------------------------------------------------------------------
# A quality-targeted preset
# -----------------------------------------------------------------------------

# For archives: one MP4 at the source's resolution (no renditions given),
# coded to a perceptual quality score instead of a rate. vmaf=95 is close to
# visually lossless; lower numbers make smaller files.
resource "transcdr_preset" "archive" {
  name = "Archive AV1"
  output = jsonencode({
    mode      = "single" # one MP4 per rendition
    codec     = "av1"
    quality   = { target = "vmaf=95" }
    bit_depth = "10bit"
    color     = "passthrough" # keep the source's colour (SDR or HDR) as it is
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
