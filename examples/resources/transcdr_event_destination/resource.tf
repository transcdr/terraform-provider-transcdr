# Signed POSTs to your server.
resource "transcdr_event_destination" "https" {
  url         = "https://example.com/hooks/transcdr"
  events      = ["job.completed", "job.failed"]
  description = "Job outcomes"

  # Change to rotate signing_secret.
  secret_version = 1
}

# Publish to an SNS topic.
resource "transcdr_event_destination" "sns" {
  topic_arn = aws_sns_topic.events.arn
  aws = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
  }
  events = ["job.completed"]
}

# Send to an SQS queue.
resource "transcdr_event_destination" "sqs" {
  queue_url = aws_sqs_queue.events.url
  aws = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
  }
}

# Deliver through a messaging connection, which holds the target and keys.
resource "transcdr_event_destination" "via_connection" {
  connection_id = transcdr_connection.triggers.id
  events        = ["*"]
}
