output "upload_to" {
  description = "Upload videos here."
  value       = "r2://${cloudflare_r2_bucket.media.name}/${var.prefix}"
}

output "connection_status" {
  description = "The connection test's outcome when it was saved: ok, or error (see last_error)."
  value       = transcdr_connection.r2.status
}

output "automation_id" {
  value = transcdr_automation.watch.id
}
