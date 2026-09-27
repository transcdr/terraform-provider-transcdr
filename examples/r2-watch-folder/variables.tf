# -----------------------------------------------------------------------------
# Inputs
# -----------------------------------------------------------------------------
#
# Three variables have no default and must be given:
#
#   cloudflare_account_id  from the Cloudflare dashboard (the account's
#                          overview page, or the URL: dash.cloudflare.com/<id>)
#   r2_access_key_id       an R2 API token's S3 credentials, created in the
#   r2_secret_access_key   dashboard under R2 -> Manage API tokens with
#                          "Object Read & Write" on this bucket
#
# Put them in a terraform.tfvars file (keep it out of version control) or pass
# them with -var. The secret is marked sensitive, so Terraform never prints it.

variable "cloudflare_account_id" {
  description = "The Cloudflare account that owns the bucket. It is also part of the R2 endpoint URL."
  type        = string
}

variable "bucket_name" {
  description = "The R2 bucket to create. Names are unique within your Cloudflare account."
  type        = string
  default     = "transcdr-media"
}

variable "bucket_location" {
  description = "Where R2 stores the bucket: apac, eeur, enam, weur or wnam (a hint; R2 serves it globally either way)."
  type        = string
  default     = "enam"
}

variable "r2_access_key_id" {
  description = <<-EOT
    The Access Key ID of an R2 API token with Object Read & Write on the
    bucket. The token is created in the dashboard rather than here because R2
    shows its secret only once, when it is created.
  EOT
  type        = string
}

variable "r2_secret_access_key" {
  description = "The Secret Access Key of that token. Transcdr stores it write-only; it is never shown again."
  type        = string
  sensitive   = true
}

variable "prefix" {
  description = "The folder Transcdr watches, e.g. \"incoming/\". Files elsewhere in the bucket are ignored."
  type        = string
  default     = "incoming/"
}

variable "pattern" {
  description = "Only files matching this glob are transcoded (`*`, `**`, `?`, `{a,b}`; case-insensitive)."
  type        = string
  default     = "**/*.{mp4,mov,mkv,webm,m4v}"
}

variable "poll_interval_seconds" {
  description = <<-EOT
    How often Transcdr lists the folder, from 60 to 86400 seconds. Shorter
    means new files start sooner; each listing is one R2 request (a class A
    operation), so very short intervals on a large folder cost a little more.
  EOT
  type        = number
  default     = 300
}

variable "settle_seconds" {
  description = <<-EOT
    A file is taken only once its size and modification time have not changed
    for this long, so a file that is still uploading is not transcoded half
    written. Raise it if your uploads are slow or come in several parts.
  EOT
  type        = number
  default     = 60
}

variable "preset" {
  description = "How each file is transcoded: a system preset slug (e.g. hls-av1-abr) or one of your preset ids."
  type        = string
  default     = "hls-av1-abr"
}

variable "output_prefix" {
  description = <<-EOT
    Where the outputs are written in the bucket, as a template ({job_id},
    {name}, {stem}, {ext}, {dir}, {date}, {automation}, {org}). With the
    default, incoming/talk.mov becomes transcoded/2026-09-27/talk/.

    Keep it outside var.prefix, or the watch would pick up the outputs and
    transcode them again.
  EOT
  type        = string
  default     = "transcoded/{date}/{stem}/"
}

variable "delete_source" {
  description = "Delete each source file once its job has completed and its outputs are delivered. The R2 token's Object Read & Write permission covers deleting."
  type        = bool
  default     = false
}
