data "transcdr_bucket_automation_setup" "this" {
  method            = "queue"
  bucket            = aws_s3_bucket.media.bucket
  prefix            = "incoming/"
  pattern           = "**/*.{mp4,mov}"
  queue_url         = aws_sqs_queue.triggers.url
  queue_arn         = aws_sqs_queue.triggers.arn
  outputs_to_bucket = true
}

# Transcdr's keys: read, write back and list the bucket; consume the queue.
resource "aws_iam_user_policy" "transcdr" {
  user   = aws_iam_user.transcdr.name
  policy = data.transcdr_bucket_automation_setup.this.iam_policy
}

# Let S3 send to the queue.
resource "aws_sqs_queue_policy" "triggers" {
  queue_url = aws_sqs_queue.triggers.url
  policy    = data.transcdr_bucket_automation_setup.this.queue_policy
}

# One notification configuration per extension.
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
  depends_on = [aws_sqs_queue_policy.triggers]
}
