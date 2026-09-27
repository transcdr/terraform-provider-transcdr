# Signed POSTs to your server. Verify the Transcdr-Signature header with
# signing_secret before trusting a delivery.
resource "transcdr_event_destination" "https" {
  url         = "https://example.com/hooks/transcdr"
  events      = ["job.completed", "job.failed"] # or leave out for every event
  description = "Job outcomes"

  # Change this number to rotate signing_secret; the old secret stops working
  # at once.
  secret_version = 1
}

# Publish to an SNS topic, to fan out to Lambda, SQS, email and more. The
# signature is in the transcdr-signature message attribute.
resource "transcdr_event_destination" "sns" {
  topic_arn = aws_sns_topic.events.arn
  aws = {
    access_key_id     = aws_iam_access_key.transcdr.id     # needs sns:Publish on the topic
    secret_access_key = aws_iam_access_key.transcdr.secret # write-only; change it to rotate
  }
  events = ["job.completed"]
}

# Send to an SQS queue your workers consume. FIFO queues (.fifo) also accept
# aws.message_group_id.
resource "transcdr_event_destination" "sqs" {
  queue_url = aws_sqs_queue.events.url
  aws = {
    access_key_id     = aws_iam_access_key.transcdr.id # needs sqs:SendMessage on the queue
    secret_access_key = aws_iam_access_key.transcdr.secret
  }
}

# Deliver through a messaging connection (sqs, sns or webhook), which holds
# the target and its credentials. The connection's health and on/off state
# then apply to this destination too.
resource "transcdr_event_destination" "via_connection" {
  connection_id = transcdr_connection.triggers.id
  events        = ["*"]
}
