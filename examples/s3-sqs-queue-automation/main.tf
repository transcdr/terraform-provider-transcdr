# =============================================================================
# S3 -> SQS -> Transcdr -> back to S3, with completion events on SNS
# =============================================================================
#
# What happens once this is applied:
#
#   1. You upload talk.mov to s3://<bucket>/incoming/.
#   2. S3 sends an "object created" event notification to the SQS queue
#      (aws_s3_bucket_notification.media, allowed by aws_sqs_queue_policy).
#   3. Transcdr reads the queue with the IAM user created here, finds the new
#      file, and starts a job with this pipeline's own preset
#      (transcdr_automation.ingest, transcdr_preset.delivery).
#   4. When the job completes, Transcdr writes the outputs back to the same
#      bucket under var.output_prefix, e.g. transcoded/talk/.
#   5. Transcdr publishes job.completed (or job.failed) to the SNS topic,
#      signed so you can verify it came from Transcdr
#      (transcdr_event_destination.completions).
#
# Why a queue: nothing has to be reachable from the internet, a backlog waits
# in the queue if anything is down, and messages that fail repeatedly land in
# the dead-letter queue for inspection. It is the recommended setup on AWS.
#
# The file is laid out in the order things depend on each other:
#
#   bucket -> queues -> generated policies -> queue policy -> notification
#          -> SNS topic -> IAM user and key -> Transcdr connections
#          -> preset -> automation -> event destination

# -----------------------------------------------------------------------------
# The bucket
# -----------------------------------------------------------------------------

# One bucket holds both the source videos (under var.prefix) and the outputs
# (under var.output_prefix). `bucket_prefix` lets AWS add a random suffix, so
# the name is unique without you choosing one.
resource "aws_s3_bucket" "media" {
  bucket_prefix = "${var.name}-"

  # `force_destroy` lets `terraform destroy` delete the bucket even when it
  # still holds videos and outputs. Remove it for a bucket you care about.
  force_destroy = true
}

# -----------------------------------------------------------------------------
# The trigger queue
# -----------------------------------------------------------------------------

# The dead-letter queue: a message Transcdr fails to act on five times (see
# maxReceiveCount below) moves here instead of being retried forever. Check it
# when a file never turns into a job. Messages are kept for 14 days, the SQS
# maximum.
resource "aws_sqs_queue" "dlq" {
  name                      = "${var.name}-dlq"
  message_retention_seconds = 1209600
}

# The queue S3 sends events to and Transcdr reads.
resource "aws_sqs_queue" "triggers" {
  name = "${var.name}-triggers"

  # While Transcdr handles a message, the message is hidden from other readers
  # for this long. Two minutes is plenty: Transcdr only starts the job, it does
  # not wait for the transcode to finish before deleting the message.
  visibility_timeout_seconds = 120

  # After five failed receives, move the message to the dead-letter queue.
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 5
  })
}

# -----------------------------------------------------------------------------
# The generated policies and notification filters
# -----------------------------------------------------------------------------

# This data source writes the JSON policies and the notification filters for
# you, exactly as the Transcdr dashboard's "Automate a bucket" wizard does. It
# runs entirely inside Terraform: nothing is sent to the Transcdr API.
#
# Its outputs, and where they are used below:
#
#   iam_policy            what Transcdr's IAM user may do: list, read and write
#                         the bucket (outputs_to_bucket = true adds write), and
#                         receive and delete messages on the queue.
#   queue_policy          lets s3.amazonaws.com send this bucket's events to
#                         the queue, and nothing else.
#   notification_filters  one entry per S3 notification configuration, built
#                         from var.prefix and var.pattern.
data "transcdr_bucket_automation_setup" "this" {
  # "queue" = S3 notifies an SQS queue that Transcdr reads. The other methods
  # are "watch" (Transcdr lists the bucket) and "webhook" (S3 notifies an SNS
  # topic that posts to Transcdr).
  method = "queue"

  bucket  = aws_s3_bucket.media.bucket
  region  = var.region
  prefix  = var.prefix
  pattern = var.pattern

  # The queue's ARN goes into the IAM and queue policies. Giving both the URL
  # and the ARN also works for queues whose URL is not an AWS one.
  queue_url = aws_sqs_queue.triggers.url
  queue_arn = aws_sqs_queue.triggers.arn

  # The outputs are delivered back to this bucket, so Transcdr also needs
  # s3:PutObject there. Set it to false when outputs go somewhere else.
  outputs_to_bucket = true
}

# Let S3 send the bucket's events to the queue. Without this policy, S3 refuses
# to save the notification configuration below.
resource "aws_sqs_queue_policy" "triggers" {
  queue_url = aws_sqs_queue.triggers.url
  policy    = data.transcdr_bucket_automation_setup.this.queue_policy
}

# Tell S3 to send an event to the queue for every new object that matches the
# filters. With the default pattern this creates five configurations, one per
# extension (.mp4, .mov, .mkv, .webm, .m4v), all under var.prefix.
#
# A bucket has exactly one notification configuration: this resource replaces
# whatever the bucket had before. If the bucket already sends notifications
# elsewhere, add those as extra blocks here.
resource "aws_s3_bucket_notification" "media" {
  bucket = aws_s3_bucket.media.id

  dynamic "queue" {
    for_each = data.transcdr_bucket_automation_setup.this.notification_filters
    content {
      id            = queue.value.id # e.g. "transcdr-mp4"
      queue_arn     = aws_sqs_queue.triggers.arn
      events        = queue.value.events # ["s3:ObjectCreated:*"]
      filter_prefix = queue.value.filter_prefix
      filter_suffix = queue.value.filter_suffix # null when the pattern has no simple suffix
    }
  }

  # S3 checks that it may send to the queue when the configuration is saved,
  # so the queue policy has to exist first.
  depends_on = [aws_sqs_queue_policy.triggers]
}

# -----------------------------------------------------------------------------
# Completion events
# -----------------------------------------------------------------------------

# The topic Transcdr publishes job.completed and job.failed to. Subscribe
# whatever should react to a finished job: a Lambda function, another SQS
# queue, an HTTPS endpoint, or email (below).
resource "aws_sns_topic" "events" {
  name = "${var.name}-events"
}

# Optional: an email for every event, when var.notification_email is set.
# `count` turns the resource on (1) or off (0).
resource "aws_sns_topic_subscription" "email" {
  count     = var.notification_email == null ? 0 : 1
  topic_arn = aws_sns_topic.events.arn
  protocol  = "email"
  endpoint  = var.notification_email
}

# -----------------------------------------------------------------------------
# Transcdr's credentials
# -----------------------------------------------------------------------------

# Transcdr signs in to AWS as this IAM user. It gets only the permissions in
# the two policies below: this bucket, this queue and this topic.
resource "aws_iam_user" "transcdr" {
  name = "${var.name}-transcdr"
}

# List, read and write the bucket; receive, delete and change the visibility
# of the queue's messages. Generated by the data source above.
resource "aws_iam_user_policy" "transcdr" {
  name   = "transcdr-bucket-and-queue"
  user   = aws_iam_user.transcdr.name
  policy = data.transcdr_bucket_automation_setup.this.iam_policy
}

# Publish to the events topic, for the event destination.
resource "aws_iam_user_policy" "events" {
  name = "transcdr-events"
  user = aws_iam_user.transcdr.name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid      = "TranscdrPublish"
      Effect   = "Allow"
      Action   = "sns:Publish"
      Resource = aws_sns_topic.events.arn
    }]
  })
}

# The access key Transcdr uses. Its secret is stored in the Terraform state,
# so keep the state somewhere private (an encrypted remote backend, not a
# public repository).
resource "aws_iam_access_key" "transcdr" {
  user = aws_iam_user.transcdr.name
}

# New access keys can take a few seconds to be accepted everywhere in AWS.
# Transcdr tests the credentials the moment a connection is saved, so wait
# first; otherwise the first test may fail and the connection starts with
# status "error" (it recovers on the next successful use).
resource "time_sleep" "iam" {
  create_duration = var.iam_propagation_wait
  depends_on      = [aws_iam_access_key.transcdr, aws_iam_user_policy.transcdr, aws_iam_user_policy.events]
}

# -----------------------------------------------------------------------------
# Transcdr: connections
# -----------------------------------------------------------------------------

# The bucket, as Transcdr sees it: where source files are read from and where
# outputs are delivered. Transcdr tests the credentials when this is saved;
# the result is in the resource's `status` ("ok" or "error") and `last_error`
# attributes.
resource "transcdr_connection" "bucket" {
  name = "${var.name} bucket"
  kind = "s3"

  config = {
    bucket = aws_s3_bucket.media.bucket
    region = var.region
    # For S3-compatible storage (R2, B2, MinIO) you would also set `endpoint`,
    # and `path_style = true` for MinIO.
  }

  # Secrets are write-only: Transcdr never returns them. Terraform keeps the
  # values from here in its state and sends them again only when they change,
  # which is also how you rotate them (create a new access key, apply).
  secrets = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
  }

  depends_on = [time_sleep.iam]
}

# The queue, as Transcdr sees it: an `sqs` messaging connection that can
# trigger automations. The region is read from the queue URL.
resource "transcdr_connection" "queue" {
  name = "${var.name} triggers"
  kind = "sqs"

  config = {
    queue_url = aws_sqs_queue.triggers.url
  }

  secrets = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
  }

  depends_on = [time_sleep.iam]
}

# -----------------------------------------------------------------------------
# Transcdr: the preset
# -----------------------------------------------------------------------------

# How every file is transcoded: an HLS ladder in var.codec. `output` is the
# part of the output specification you care about; everything else takes the
# API's defaults, which `resolved_output` shows. Changing it later updates the
# preset in place, and a field you remove goes back to its default.
#
# To use a system preset instead, drop this resource and set the automation's
# `preset` to a slug such as "hls-av1-abr".
resource "transcdr_preset" "delivery" {
  name        = "${var.name} delivery"
  description = "HLS ladder for ${var.name}, managed by Terraform"

  output = jsonencode({
    mode  = "hls"
    codec = var.codec
    renditions = [
      { width = 1920, height = 1080 },
      { width = 1280, height = 720 },
      { width = 854, height = 480 },
    ]
  })

  metadata = {
    pipeline = var.name
  }
}

# -----------------------------------------------------------------------------
# Transcdr: the automation
# -----------------------------------------------------------------------------

# "When a file lands in the bucket, transcode it like this, deliver it there."
resource "transcdr_automation" "ingest" {
  name = var.name

  # "queue": consume the SQS connection. Each S3 event on the queue becomes a
  # job; the message is deleted once the job is created. The same queue also
  # accepts {"path": "incoming/talk.mov"} messages and POST /v1/jobs bodies,
  # if you want to trigger jobs by hand.
  trigger               = "queue"
  trigger_connection_id = transcdr_connection.queue.id

  # Which files: the bucket connection, under the prefix, matching the
  # pattern. Keys from the S3 events are checked against both, so a file that
  # slipped past the notification filters is still skipped.
  source = {
    connection_id = transcdr_connection.bucket.id
    prefix        = var.prefix
    pattern       = var.pattern
  }

  # How: the preset above, by id (a system preset slug also works). To change
  # a few settings for this automation only, add
  # `output = jsonencode({ segment_seconds = 4 })`: those fields are merged
  # over the preset's.
  preset = transcdr_preset.delivery.id

  # Where: back into the bucket, under the output template. Leave the whole
  # block out to keep outputs in Transcdr's own storage and download them
  # through the API instead; removing it later stops delivery from the next
  # job on, without replacing the automation.
  destination = {
    connection_id = transcdr_connection.bucket.id
    prefix        = var.output_prefix
  }

  # Added to every job this automation creates (alongside automation_id and
  # source_path), so you can filter jobs by pipeline in the API.
  metadata = {
    pipeline = var.name
  }

  # Other options, left at their defaults here:
  #   after_success = "delete"   remove the source once its job completes
  #                              (the IAM policy then needs s3:DeleteObject:
  #                              set delete_source = true on the data source)
  #   priority      = "high"     on plans with priority processing
  #   webhook_url   = "https://…" a per-job webhook for every job
  #   enabled       = false      pause the automation without deleting it

  # Only start reading the queue once the notification exists, so no event is
  # missed between the two.
  depends_on = [aws_s3_bucket_notification.media]
}

# -----------------------------------------------------------------------------
# Transcdr: completion events
# -----------------------------------------------------------------------------

# Send job.completed and job.failed to the SNS topic. Each message carries a
# `transcdr-signature` message attribute, signed with this destination's
# `signing_secret`; verify it before trusting a message (the Transcdr SDKs
# have a verifySnsSqsSignature helper).
resource "transcdr_event_destination" "completions" {
  description = "${var.name}: completions"
  topic_arn   = aws_sns_topic.events.arn

  # The same IAM user publishes; it has sns:Publish on this topic only. The
  # secret access key is write-only, like a connection's secrets.
  aws = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
  }

  # Omit `events` (or use ["*"]) to receive every event type.
  events = ["job.completed", "job.failed"]

  depends_on = [time_sleep.iam]
}
