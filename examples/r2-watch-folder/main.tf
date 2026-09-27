# A Cloudflare R2 bucket watched by Transcdr.
#
# R2's event notifications do not reach SQS, so Transcdr watches the folder:
# it lists var.prefix every var.poll_interval_seconds, takes each file that
# has not changed for var.settle_seconds, and delivers the outputs back to
# the same bucket under var.output_prefix.
#
# R2 speaks the S3 API, so the connection is an `s3` one with R2's endpoint
# and region `auto`. Its keys are an R2 API token's S3 credentials, created
# in the dashboard (R2 -> Manage API tokens, "Object Read & Write" on this
# bucket), passed in as variables.

resource "cloudflare_r2_bucket" "media" {
  account_id = var.cloudflare_account_id
  name       = var.bucket_name
  location   = var.bucket_location
}

resource "transcdr_connection" "r2" {
  name = "R2 ${cloudflare_r2_bucket.media.name}"
  kind = "s3"
  config = {
    bucket   = cloudflare_r2_bucket.media.name
    region   = "auto"
    endpoint = "https://${var.cloudflare_account_id}.r2.cloudflarestorage.com"
  }
  secrets = {
    access_key_id     = var.r2_access_key_id
    secret_access_key = var.r2_secret_access_key
  }
}

resource "transcdr_automation" "watch" {
  name    = "Watch ${cloudflare_r2_bucket.media.name}"
  trigger = "watch"

  source = {
    connection_id = transcdr_connection.r2.id
    prefix        = var.prefix
    pattern       = var.pattern
  }
  poll_interval_seconds = var.poll_interval_seconds
  settle_seconds        = var.settle_seconds

  preset = var.preset

  destination = {
    connection_id = transcdr_connection.r2.id
    prefix        = var.output_prefix
  }

  # Remove the source once its outputs are delivered.
  after_success = var.delete_source ? "delete" : "keep"
}
