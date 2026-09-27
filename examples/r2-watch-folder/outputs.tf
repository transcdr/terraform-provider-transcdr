# -----------------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------------

output "upload_to" {
  description = "Upload videos here, e.g. with rclone, the aws CLI (--endpoint-url https://<account>.r2.cloudflarestorage.com) or the dashboard."
  value       = "r2://${cloudflare_r2_bucket.media.name}/${var.prefix}"
}

output "connection_status" {
  description = <<-EOT
    The result of the credential test Transcdr ran when the connection was
    saved: "ok", or "error" with the reason in the connection's last_error
    (terraform state show transcdr_connection.r2). A refresh shows the
    current value.
  EOT
  value       = transcdr_connection.r2.status
}

output "automation_id" {
  description = "The automation's id (aut_…). POST /v1/automations/{id}/run lists the folder now instead of at the next poll."
  value       = transcdr_automation.watch.id
}
