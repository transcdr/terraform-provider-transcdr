# S3 -> SNS topic -> HTTPS subscription -> the automation's hook URL.
#
# New files under var.prefix publish an S3 event to an SNS topic. The topic
# has an HTTPS subscription to the automation's secret hook_url, which
# confirms the subscription by itself and turns each notification into a
# job. Other consumers can subscribe to the same topic: that is the fan-out.

resource "aws_s3_bucket" "media" {
  bucket_prefix = "${var.name}-"
  force_destroy = true
}

resource "aws_sns_topic" "uploads" {
  name = "${var.name}-uploads"
}

data "transcdr_bucket_automation_setup" "this" {
  method    = "webhook"
  bucket    = aws_s3_bucket.media.bucket
  region    = var.region
  prefix    = var.prefix
  pattern   = var.pattern
  topic_arn = aws_sns_topic.uploads.arn
}

# Lets S3 publish the bucket's events to the topic.
resource "aws_sns_topic_policy" "uploads" {
  arn    = aws_sns_topic.uploads.arn
  policy = data.transcdr_bucket_automation_setup.this.topic_policy
}

resource "aws_s3_bucket_notification" "media" {
  bucket = aws_s3_bucket.media.id

  dynamic "topic" {
    for_each = data.transcdr_bucket_automation_setup.this.notification_filters
    content {
      id            = topic.value.id
      topic_arn     = aws_sns_topic.uploads.arn
      events        = topic.value.events
      filter_prefix = topic.value.filter_prefix
      filter_suffix = topic.value.filter_suffix
    }
  }

  depends_on = [aws_sns_topic_policy.uploads]
}

# Transcdr reads the source files with its own keys (read-only: outputs stay
# in Transcdr's storage here).
resource "aws_iam_user" "transcdr" {
  name = "${var.name}-transcdr"
}

resource "aws_iam_user_policy" "transcdr" {
  name   = "transcdr-read-bucket"
  user   = aws_iam_user.transcdr.name
  policy = data.transcdr_bucket_automation_setup.this.iam_policy
}

resource "aws_iam_access_key" "transcdr" {
  user = aws_iam_user.transcdr.name
}

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
  depends_on = [aws_iam_user_policy.transcdr]
}

resource "transcdr_automation" "hook" {
  name    = var.name
  trigger = "hook"

  source = {
    connection_id = transcdr_connection.bucket.id
    prefix        = var.prefix
    pattern       = var.pattern
  }

  preset = var.preset

  # Change to rotate the hook URL; the subscription below follows it.
  hook_token_version = var.hook_token_version
}

# Transcdr confirms the subscription itself when SNS sends the confirmation.
resource "aws_sns_topic_subscription" "transcdr" {
  topic_arn              = aws_sns_topic.uploads.arn
  protocol               = "https"
  endpoint               = transcdr_automation.hook.hook_url
  endpoint_auto_confirms = true
}
