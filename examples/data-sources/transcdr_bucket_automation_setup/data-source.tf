# Everything AWS needs so new files in a bucket reach a queue automation,
# generated inside Terraform with no API call, identical to the dashboard's
# "Automate a bucket" wizard.
data "transcdr_bucket_automation_setup" "this" {
  method            = "queue" # or "watch", or "webhook" (S3 -> SNS -> hook_url)
  bucket            = aws_s3_bucket.media.bucket
  prefix            = "incoming/"
  pattern           = "**/*.{mp4,mov}" # becomes one notification per extension
  queue_url         = aws_sqs_queue.triggers.url
  queue_arn         = aws_sqs_queue.triggers.arn
  outputs_to_bucket = true # outputs are delivered back: adds write access
}

# Transcdr's keys: list, read and write the bucket; consume the queue.
resource "aws_iam_user_policy" "transcdr" {
  user   = aws_iam_user.transcdr.name
  policy = data.transcdr_bucket_automation_setup.this.iam_policy
}

# Let S3 send the bucket's events to the queue.
resource "aws_sqs_queue_policy" "triggers" {
  queue_url = aws_sqs_queue.triggers.url
  policy    = data.transcdr_bucket_automation_setup.this.queue_policy
}

# One notification configuration per extension. This replaces the bucket's
# existing notification configuration, if it had one.
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
  # S3 checks the queue policy when the notification is saved.
  depends_on = [aws_sqs_queue_policy.triggers]
}
