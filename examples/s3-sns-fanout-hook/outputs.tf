output "bucket" {
  description = "Upload videos here, under the prefix."
  value       = "s3://${aws_s3_bucket.media.bucket}/${var.prefix}"
}

output "topic_arn" {
  description = "Subscribe other consumers (a queue, a Lambda) to the same events here."
  value       = aws_sns_topic.uploads.arn
}

output "automation_id" {
  value = transcdr_automation.hook.id
}

output "hook_url" {
  description = "The automation's secret push URL. It is a credential."
  value       = transcdr_automation.hook.hook_url
  sensitive   = true
}
