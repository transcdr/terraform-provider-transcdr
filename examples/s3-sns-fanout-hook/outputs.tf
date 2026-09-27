# -----------------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------------

output "bucket" {
  description = "Upload videos here, under the prefix: aws s3 cp talk.mp4 \"$(terraform output -raw bucket)talk.mp4\""
  value       = "s3://${aws_s3_bucket.media.bucket}/${var.prefix}"
}

output "topic_arn" {
  description = "Subscribe other consumers (a queue, a Lambda function) to the same upload events here."
  value       = aws_sns_topic.uploads.arn
}

output "automation_id" {
  description = "The automation's id (aut_…). GET /v1/automations/{id}/items lists the files it has processed."
  value       = transcdr_automation.hook.id
}

# The hook URL is a credential. Show it with `terraform output -raw hook_url`,
# e.g. to test it by hand:
#
#   curl -X POST "$(terraform output -raw hook_url)" \
#     -H 'Content-Type: application/json' -d '{"path": "incoming/talk.mp4"}'
output "hook_url" {
  description = "The automation's secret push URL. Anyone with it can make Transcdr process files in the bucket."
  value       = transcdr_automation.hook.hook_url
  sensitive   = true
}
