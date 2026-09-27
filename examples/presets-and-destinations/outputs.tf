# -----------------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------------

output "plan" {
  description = "The organization's plan, e.g. \"Growth\". Its limits (max_resolution, features) are on data.transcdr_organization.this."
  value       = data.transcdr_organization.this.plan_name
}

# Use a preset from anywhere: a job (`"preset": "pre_…"` or the slug), an
# automation's `preset` argument, or the dashboard.
output "presets" {
  description = "Use these ids (or slugs) as a job's or automation's preset."
  value = {
    cbr_broadcast = transcdr_preset.cbr_broadcast.id
    web_h265_abr  = transcdr_preset.web_h265_abr.id
    archive       = transcdr_preset.archive.id
  }
}

# resolved_output is JSON text; jsondecode turns it into an object so
# `terraform output cbr_resolved` prints it readably.
output "cbr_resolved" {
  description = "The CBR preset's full output specification, with every default filled in."
  value       = jsondecode(transcdr_preset.cbr_broadcast.resolved_output)
}

# Returned once, when the destination is created (or its secret rotated). Read
# it with `terraform output -raw signing_secret` and give it to your receiver.
output "signing_secret" {
  description = "Verify the Transcdr-Signature header of each delivery with this."
  value       = transcdr_event_destination.https.signing_secret
  sensitive   = true
}
