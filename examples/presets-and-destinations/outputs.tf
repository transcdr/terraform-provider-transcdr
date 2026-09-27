output "plan" {
  value = data.transcdr_organization.this.plan_name
}

output "presets" {
  description = "Use these ids (or slugs) as a job's or automation's preset."
  value = {
    cbr_broadcast = transcdr_preset.cbr_broadcast.id
    web_h265_abr  = transcdr_preset.web_h265_abr.id
    archive       = transcdr_preset.archive.id
  }
}

output "cbr_resolved" {
  description = "The CBR preset's full output specification, with the defaults filled in."
  value       = jsondecode(transcdr_preset.cbr_broadcast.resolved_output)
}

output "signing_secret" {
  description = "Verify the Transcdr-Signature header of each delivery with this."
  value       = transcdr_event_destination.https.signing_secret
  sensitive   = true
}
