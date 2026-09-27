# -----------------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------------
#
# Print them with `terraform output`, or one value with
# `terraform output -raw bucket`.

output "bucket" {
  description = "Upload videos here, under the prefix: aws s3 cp talk.mov \"$(terraform output -raw bucket)talk.mov\""
  value       = "s3://${aws_s3_bucket.media.bucket}/${var.prefix}"
}

output "bucket_name" {
  description = "The bucket's name, for tools that want it without the s3:// form."
  value       = aws_s3_bucket.media.bucket
}

output "automation_id" {
  description = "The automation's id (aut_…). List the files it has processed with GET /v1/automations/{id}/items."
  value       = transcdr_automation.ingest.id
}

output "preset_id" {
  description = "The pipeline's preset (pre_…). Jobs you start by hand can use it too: {\"preset\": \"<id>\"}."
  value       = transcdr_preset.delivery.id
}

output "queue_url" {
  description = "The trigger queue. If a file never becomes a job, look at the dead-letter queue next to it."
  value       = aws_sqs_queue.triggers.url
}

output "events_topic_arn" {
  description = "Subscribe to this topic to react to finished jobs (Lambda, SQS, HTTPS, email)."
  value       = aws_sns_topic.events.arn
}

output "filters_note" {
  description = "How the pattern became S3 notification filters, as the dashboard explains it."
  value       = data.transcdr_bucket_automation_setup.this.filters_note
}

# Sensitive: Terraform prints <sensitive> unless you ask for it explicitly with
# `terraform output -raw signing_secret`.
output "signing_secret" {
  description = "Verify the transcdr-signature attribute of each SNS message with this secret."
  value       = transcdr_event_destination.completions.signing_secret
  sensitive   = true
}
