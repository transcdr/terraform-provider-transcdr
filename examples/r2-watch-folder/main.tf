# =============================================================================
# A Cloudflare R2 bucket, watched by Transcdr
# =============================================================================
#
# What happens once this is applied:
#
#   1. You upload talk.mov to the bucket, under incoming/.
#   2. Every var.poll_interval_seconds, Transcdr lists incoming/ and notices
#      the new file.
#   3. Once the file has not changed for var.settle_seconds (so it has
#      finished uploading), Transcdr starts a job with var.preset.
#   4. When the job completes, the outputs are written back to the bucket
#      under var.output_prefix, and, with var.delete_source, the source file
#      is removed.
#
# Why "watch": R2's event notifications cannot be sent to an SQS queue, so the
# queue-based setup of the S3 examples does not apply. Watching needs nothing
# but read and list access, and works with any storage Transcdr supports. The
# price is latency: a new file waits up to one poll interval plus the settle
# time before its job starts.
#
# Each version of each file is processed exactly once: re-uploading the same
# file under the same name does not start a second job unless its content
# changed.

# -----------------------------------------------------------------------------
# The bucket
# -----------------------------------------------------------------------------

resource "cloudflare_r2_bucket" "media" {
  account_id = var.cloudflare_account_id
  name       = var.bucket_name
  location   = var.bucket_location
}

# -----------------------------------------------------------------------------
# Transcdr: the connection
# -----------------------------------------------------------------------------

# R2 speaks the S3 API, so the connection is an `s3` one pointed at R2's
# endpoint, with region "auto" (R2's only region). The same pattern works for
# Backblaze B2, Wasabi and MinIO: set their endpoint instead, and for MinIO
# add `path_style = true`.
resource "transcdr_connection" "r2" {
  name = "R2 ${cloudflare_r2_bucket.media.name}"
  kind = "s3"

  config = {
    bucket   = cloudflare_r2_bucket.media.name
    region   = "auto"
    endpoint = "https://${var.cloudflare_account_id}.r2.cloudflarestorage.com"

    # Optional: make every path relative to a folder, e.g. root = "videos/".
    # prefix and output_prefix below would then be inside that folder.
  }

  # Write-only: Transcdr never returns these. Terraform keeps them in its
  # state and sends them again only when they change; to rotate, create a new
  # R2 token, update the variables and apply.
  secrets = {
    access_key_id     = var.r2_access_key_id
    secret_access_key = var.r2_secret_access_key
  }

  # If R2 rejects the keys, the connection is still created, with
  # status = "error" and the reason in last_error (see outputs.tf). Transcdr
  # also turns a connection off after a permanent failure while it is in use;
  # the next `terraform plan` then shows `enabled` going back to true, and
  # applying tests it again and turns it back on.
}

# -----------------------------------------------------------------------------
# Transcdr: the watch automation
# -----------------------------------------------------------------------------

resource "transcdr_automation" "watch" {
  name = "Watch ${cloudflare_r2_bucket.media.name}"

  # "watch": Transcdr lists the source on a schedule. The connection must be
  # listable (its capabilities.watch attribute is true for s3).
  trigger = "watch"

  source = {
    connection_id = transcdr_connection.r2.id
    prefix        = var.prefix
    pattern       = var.pattern
  }
  poll_interval_seconds = var.poll_interval_seconds
  settle_seconds        = var.settle_seconds

  preset = var.preset

  # Deliver to the same bucket, under the output template.
  destination = {
    connection_id = transcdr_connection.r2.id
    prefix        = var.output_prefix
  }

  # "delete" removes the source file after a successful job; "keep" (the
  # default) leaves it where it is.
  after_success = var.delete_source ? "delete" : "keep"
}
