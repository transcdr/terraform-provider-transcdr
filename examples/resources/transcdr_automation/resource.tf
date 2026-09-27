# Watch a folder: Transcdr lists it every poll interval, waits until a new
# file has stopped changing (the settle time), transcodes it, and delivers the
# outputs next to the source.
resource "transcdr_automation" "watch" {
  name    = "Ingest to HLS"
  trigger = "watch"

  source = {
    connection_id = transcdr_connection.ingest.id
    prefix        = "incoming/"      # only files under this folder
    pattern       = "**/*.{mp4,mov}" # and matching this glob (case-insensitive)
  }
  poll_interval_seconds = 300 # 60 to 86400
  settle_seconds        = 60  # how long a file must stay unchanged

  # A system preset slug or a transcdr_preset id, plus optional overrides
  # merged over it (objects merge, arrays and values replace).
  preset = "hls-av1-abr"
  output = jsonencode({
    segment_seconds = 6
  })

  # Deliver outputs to a writable connection. Keep the prefix outside the
  # watched folder, or the outputs would be transcoded again. Template
  # variables: {job_id} {name} {stem} {ext} {dir} {date} {automation} {org}.
  destination = {
    connection_id = transcdr_connection.ingest.id
    prefix        = "transcoded/{date}/{stem}/"
  }
  after_success = "keep" # or "delete" the source once its job completes
  metadata      = { pipeline = "ingest" }
}

# Consume S3 event notifications from an SQS queue: seconds of latency, no
# public endpoint, and a backlog that survives outages. The
# transcdr_bucket_automation_setup data source writes the queue policy and
# the bucket notification.
resource "transcdr_automation" "queue" {
  name                  = "Uploads (queue)"
  trigger               = "queue"
  trigger_connection_id = transcdr_connection.triggers.id

  source = {
    connection_id = transcdr_connection.ingest.id
    prefix        = "uploads/"
  }
  preset = transcdr_preset.cbr_broadcast.id
}

# Take pushes at a secret URL (hook_url): an SNS HTTPS subscription, a MinIO
# webhook target, or your own code posting {"path": "incoming/talk.mov"}.
resource "transcdr_automation" "hook" {
  name    = "Uploads (hook)"
  trigger = "hook"
  source = {
    connection_id = transcdr_connection.ingest.id
  }
  preset = "hls-h264-abr"

  # hook_url is a credential. Change this number to rotate it.
  hook_token_version = 1
}

# Transcdr confirms the SNS subscription by itself.
resource "aws_sns_topic_subscription" "transcdr" {
  topic_arn              = aws_sns_topic.uploads.arn
  protocol               = "https"
  endpoint               = transcdr_automation.hook.hook_url
  endpoint_auto_confirms = true
}
