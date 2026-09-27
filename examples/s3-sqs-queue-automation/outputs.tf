output "bucket" {
  description = "Upload videos here, under the prefix."
  value       = "s3://${aws_s3_bucket.media.bucket}/${var.prefix}"
}

output "bucket_name" {
  value = aws_s3_bucket.media.bucket
}

output "automation_id" {
  value = transcdr_automation.ingest.id
}

output "queue_url" {
  value = aws_sqs_queue.triggers.url
}

output "events_topic_arn" {
  value = aws_sns_topic.events.arn
}

output "filters_note" {
  description = "How the pattern became S3 notification filters."
  value       = data.transcdr_bucket_automation_setup.this.filters_note
}

output "signing_secret" {
  description = "Verify the transcdr-signature attribute of each SNS message with this."
  value       = transcdr_event_destination.completions.signing_secret
  sensitive   = true
}
