# S3 -> SQS -> Transcdr -> back to S3, with completion events on SNS.
#
# Uploads under var.prefix send an S3 event notification to an SQS queue (with
# a dead-letter queue). Transcdr consumes the queue with its own IAM user,
# transcodes each file with var.preset, delivers the outputs back to the
# bucket under var.output_prefix, and publishes job.completed and job.failed
# to an SNS topic.

resource "aws_s3_bucket" "media" {
  bucket_prefix = "${var.name}-"
  force_destroy = true
}

# ---- The trigger queue ---------------------------------------------------------------

resource "aws_sqs_queue" "dlq" {
  name                      = "${var.name}-dlq"
  message_retention_seconds = 1209600
}

resource "aws_sqs_queue" "triggers" {
  name                       = "${var.name}-triggers"
  visibility_timeout_seconds = 120
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 5
  })
}

# The policies and the notification filters, exactly as the dashboard's
# "Automate a bucket" generates them. Nothing is sent to the API.
data "transcdr_bucket_automation_setup" "this" {
  method            = "queue"
  bucket            = aws_s3_bucket.media.bucket
  region            = var.region
  prefix            = var.prefix
  pattern           = var.pattern
  queue_url         = aws_sqs_queue.triggers.url
  queue_arn         = aws_sqs_queue.triggers.arn
  outputs_to_bucket = true
}

# Lets S3 send the bucket's events to the queue.
resource "aws_sqs_queue_policy" "triggers" {
  queue_url = aws_sqs_queue.triggers.url
  policy    = data.transcdr_bucket_automation_setup.this.queue_policy
}

resource "aws_s3_bucket_notification" "media" {
  bucket = aws_s3_bucket.media.id

  dynamic "queue" {
    for_each = data.transcdr_bucket_automation_setup.this.notification_filters
    content {
      id            = queue.value.id
      queue_arn     = aws_sqs_queue.triggers.arn
      events        = queue.value.events
      filter_prefix = queue.value.filter_prefix
      filter_suffix = queue.value.filter_suffix
    }
  }

  # S3 checks that it may send to the queue when the notification is saved.
  depends_on = [aws_sqs_queue_policy.triggers]
}

# ---- Completion events ------------------------------------------------------------------

resource "aws_sns_topic" "events" {
  name = "${var.name}-events"
}

resource "aws_sns_topic_subscription" "email" {
  count     = var.notification_email == null ? 0 : 1
  topic_arn = aws_sns_topic.events.arn
  protocol  = "email"
  endpoint  = var.notification_email
}

# ---- Transcdr's credentials ---------------------------------------------------------------

resource "aws_iam_user" "transcdr" {
  name = "${var.name}-transcdr"
}

# List, read and write back to the bucket; consume the queue.
resource "aws_iam_user_policy" "transcdr" {
  name   = "transcdr-bucket-and-queue"
  user   = aws_iam_user.transcdr.name
  policy = data.transcdr_bucket_automation_setup.this.iam_policy
}

# Publish events to the topic.
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

resource "aws_iam_access_key" "transcdr" {
  user = aws_iam_user.transcdr.name
}

# New access keys take a few seconds to work everywhere.
resource "time_sleep" "iam" {
  create_duration = var.iam_propagation_wait
  depends_on      = [aws_iam_access_key.transcdr, aws_iam_user_policy.transcdr, aws_iam_user_policy.events]
}

# ---- Transcdr -------------------------------------------------------------------------------

resource "transcdr_connection" "bucket" {
  name = "${var.name} bucket"
  kind = "s3"
  config = {
    bucket = aws_s3_bucket.media.bucket
    region = var.region
  }
  secrets = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
  }
  depends_on = [time_sleep.iam]
}

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

resource "transcdr_automation" "ingest" {
  name                  = var.name
  trigger               = "queue"
  trigger_connection_id = transcdr_connection.queue.id

  source = {
    connection_id = transcdr_connection.bucket.id
    prefix        = var.prefix
    pattern       = var.pattern
  }

  preset = var.preset

  destination = {
    connection_id = transcdr_connection.bucket.id
    prefix        = var.output_prefix
  }

  metadata = {
    pipeline = var.name
  }

  # The notification must exist before the automation starts reading the queue.
  depends_on = [aws_s3_bucket_notification.media]
}

resource "transcdr_event_destination" "completions" {
  description = "${var.name}: completions"
  topic_arn   = aws_sns_topic.events.arn
  aws = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
  }
  events     = ["job.completed", "job.failed"]
  depends_on = [time_sleep.iam]
}
