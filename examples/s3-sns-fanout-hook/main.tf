# =============================================================================
# S3 -> SNS topic -> HTTPS subscription -> the automation's hook URL
# =============================================================================
#
# What happens once this is applied:
#
#   1. You upload talk.mp4 to s3://<bucket>/incoming/.
#   2. S3 publishes an "object created" event to the SNS topic.
#   3. SNS delivers it, over HTTPS, to every subscriber. One subscriber is the
#      Transcdr automation's secret hook_url.
#   4. Transcdr unwraps the SNS envelope, decodes the S3 key, checks it against
#      the prefix and pattern, and starts a job with var.preset.
#
# Why a topic rather than a queue: other systems can subscribe to the same
# events (a queue for your own pipeline, a Lambda function, an audit log).
# That is the fan-out. The trade-off: SNS needs to reach Transcdr's public
# hook URL, and there is no queue holding a backlog if either side is down
# (SNS retries for a while, then drops the message).
#
# Outputs stay in Transcdr's storage here: the automation has no
# destination. Fetch them through the API or subscribe to job events. See the
# s3-sqs-queue-automation example for delivering outputs back to the bucket.

# -----------------------------------------------------------------------------
# The bucket and the topic
# -----------------------------------------------------------------------------

resource "aws_s3_bucket" "media" {
  bucket_prefix = "${var.name}-"
  # Lets `terraform destroy` remove a bucket that still has files in it.
  force_destroy = true
}

# The topic S3 publishes the bucket's events to.
resource "aws_sns_topic" "uploads" {
  name = "${var.name}-uploads"
}

# -----------------------------------------------------------------------------
# The generated policies and notification filters
# -----------------------------------------------------------------------------

# Generated locally, exactly as the dashboard's "Automate a bucket" does it;
# nothing is sent to the Transcdr API. With method = "webhook" and a topic:
#
#   topic_policy          lets s3.amazonaws.com publish this bucket's events
#                         to the topic, and nothing else.
#   notification_filters  one S3 notification configuration per extension.
#   iam_policy            read-only access to the bucket for Transcdr (list
#                         and get), since outputs are not written back here.
data "transcdr_bucket_automation_setup" "this" {
  method    = "webhook"
  bucket    = aws_s3_bucket.media.bucket
  region    = var.region
  prefix    = var.prefix
  pattern   = var.pattern
  topic_arn = aws_sns_topic.uploads.arn
}

# Allow S3 to publish to the topic. S3 checks this when the notification
# configuration is saved.
resource "aws_sns_topic_policy" "uploads" {
  arn    = aws_sns_topic.uploads.arn
  policy = data.transcdr_bucket_automation_setup.this.topic_policy
}

# Send "object created" events under the prefix, with the pattern's
# extensions, to the topic. This replaces any notification configuration the
# bucket already has.
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

# -----------------------------------------------------------------------------
# Transcdr's read-only access to the bucket
# -----------------------------------------------------------------------------

# Transcdr reads the source files with this user's keys. The hook only tells
# Transcdr which file is new; the file itself is read from the bucket.
resource "aws_iam_user" "transcdr" {
  name = "${var.name}-transcdr"
}

resource "aws_iam_user_policy" "transcdr" {
  name   = "transcdr-read-bucket"
  user   = aws_iam_user.transcdr.name
  policy = data.transcdr_bucket_automation_setup.this.iam_policy
}

# The key's secret is kept in the Terraform state: keep the state private.
resource "aws_iam_access_key" "transcdr" {
  user = aws_iam_user.transcdr.name
}

# -----------------------------------------------------------------------------
# Transcdr: the connection and the hook automation
# -----------------------------------------------------------------------------

# The bucket as Transcdr sees it. Secrets are write-only; Terraform keeps them
# in state and sends them again only when they change.
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

# "hook": Transcdr waits for pushes at the automation's hook_url. Besides SNS
# notifications it accepts {"path": "incoming/talk.mp4"}, {"paths": [...]}
# and S3-style Records[] bodies, so a script or MinIO can call it too.
resource "transcdr_automation" "hook" {
  name    = var.name
  trigger = "hook"

  # Which files: in the bucket connection, under the prefix, matching the
  # pattern. An event for any other key is ignored, and is not an error.
  source = {
    connection_id = transcdr_connection.bucket.id
    prefix        = var.prefix
    pattern       = var.pattern
  }

  preset = var.preset

  # Change var.hook_token_version to rotate hook_url. Setting it on the first
  # apply does not rotate anything; only a change does.
  hook_token_version = var.hook_token_version
}

# -----------------------------------------------------------------------------
# The subscription that connects SNS to Transcdr
# -----------------------------------------------------------------------------

# SNS first sends a SubscriptionConfirmation to the endpoint. Transcdr
# confirms it by itself (it only follows confirmation links on
# sns.<region>.amazonaws.com), so with endpoint_auto_confirms = true Terraform
# waits for that and the subscription becomes active within seconds.
#
# hook_url is sensitive, so Terraform shows the endpoint as (sensitive value)
# in plans. When the URL is rotated, the endpoint changes and Terraform
# replaces this subscription.
resource "aws_sns_topic_subscription" "transcdr" {
  topic_arn              = aws_sns_topic.uploads.arn
  protocol               = "https"
  endpoint               = transcdr_automation.hook.hook_url
  endpoint_auto_confirms = true
}

# To fan out further, subscribe more consumers to aws_sns_topic.uploads, e.g.
#
#   resource "aws_sns_topic_subscription" "audit" {
#     topic_arn = aws_sns_topic.uploads.arn
#     protocol  = "sqs"
#     endpoint  = aws_sqs_queue.audit.arn
#   }
