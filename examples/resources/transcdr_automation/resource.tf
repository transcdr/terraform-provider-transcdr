# Watch a folder, transcode new files, deliver the outputs next to them.
resource "transcdr_automation" "watch" {
  name    = "Ingest to HLS"
  trigger = "watch"

  source = {
    connection_id = transcdr_connection.ingest.id
    prefix        = "incoming/"
    pattern       = "**/*.{mp4,mov}"
  }
  poll_interval_seconds = 300
  settle_seconds        = 60

  preset = "hls-av1-abr"
  output = jsonencode({
    segment_seconds = 6
  })

  destination = {
    connection_id = transcdr_connection.ingest.id
    prefix        = "transcoded/{date}/{stem}/"
  }
  after_success = "keep"
  metadata      = { pipeline = "ingest" }
}

# Consume S3 event notifications from an SQS queue.
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

# Take pushes at a secret URL, e.g. an SNS HTTPS subscription.
resource "transcdr_automation" "hook" {
  name    = "Uploads (hook)"
  trigger = "hook"
  source = {
    connection_id = transcdr_connection.ingest.id
  }
  preset = "hls-h264-abr"

  # Change to rotate hook_url.
  hook_token_version = 1
}

resource "aws_sns_topic_subscription" "transcdr" {
  topic_arn              = aws_sns_topic.uploads.arn
  protocol               = "https"
  endpoint               = transcdr_automation.hook.hook_url
  endpoint_auto_confirms = true
}
