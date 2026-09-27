# -----------------------------------------------------------------------------
# Inputs
# -----------------------------------------------------------------------------
#
# Every variable has a working default, so `terraform apply` with no -var flags
# builds a complete pipeline in us-east-1. Override them on the command line
# (`-var name=acme-ingest`) or in a terraform.tfvars file.

variable "region" {
  description = "The AWS region of the bucket and the queue. Transcdr reads the region from the queue URL, so nothing else needs to know it."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = <<-EOT
    A name for everything this creates: the queues, the SNS topic, the IAM
    user and the Transcdr connections and automation all start with it. The
    bucket gets a random suffix too, because bucket names are global across
    every AWS account. Use a different name for each copy of this module in
    the same account and region.
  EOT
  type        = string
  default     = "transcdr-ingest"
}

variable "prefix" {
  description = <<-EOT
    Only files uploaded under this prefix are transcoded, e.g. "incoming/".
    It becomes the S3 notification's prefix filter, so uploads elsewhere in
    the bucket never reach the queue. Keep the trailing slash.
  EOT
  type        = string
  default     = "incoming/"
}

variable "pattern" {
  description = <<-EOT
    Only files matching this glob are transcoded. The syntax is `*` within a
    folder, `**` across folders, `?` for one character and `{a,b}` for
    alternatives; matching is case-insensitive in Transcdr.

    The pattern also shapes the S3 notification: a pattern ending in *.ext
    becomes a suffix filter, and one ending in *.{a,b,c} becomes one
    notification per extension. S3 suffix filters are case-sensitive, so a
    file named CLIP.MP4 would not be sent for ".mp4". Any other pattern
    filters on the prefix only, and Transcdr skips the files that do not
    match.
  EOT
  type        = string
  default     = "**/*.{mp4,mov,mkv,webm,m4v}"
}

variable "codec" {
  description = <<-EOT
    The video codec of the pipeline's preset (transcdr_preset.delivery):
    "h264" plays everywhere, "av1" is about half the size at the same quality
    on current devices.
  EOT
  type        = string
  default     = "h264"

  validation {
    condition     = contains(["h264", "h265", "av1"], var.codec)
    error_message = "codec must be h264, h265 or av1."
  }
}

variable "output_prefix" {
  description = <<-EOT
    Where the outputs are written in the same bucket, as a template. The
    variables are {job_id}, {name} (the file name), {stem} (the name without
    its extension), {ext}, {dir} (the file's folder), {date} (YYYY-MM-DD),
    {automation} and {org}. With the default, incoming/talk.mov produces
    transcoded/talk/master.m3u8 and its renditions.

    Keep it outside var.prefix: outputs written under the watched prefix
    would be sent to the queue and transcoded again.
  EOT
  type        = string
  default     = "transcoded/{stem}/"
}

variable "notification_email" {
  description = <<-EOT
    Optional: an email address subscribed to the completion topic, so you get
    an email for every finished or failed job. AWS sends a confirmation email
    first; nothing arrives until the link in it is clicked. Leave it null for
    no email subscription.
  EOT
  type        = string
  default     = null
}

variable "iam_propagation_wait" {
  description = <<-EOT
    How long to wait after creating Transcdr's IAM access key before Transcdr
    tries it. New keys can take a few seconds to work everywhere in AWS, and
    Transcdr tests a connection's credentials when it is saved. A Go duration
    such as "15s"; "0s" skips the wait (fine for LocalStack).
  EOT
  type        = string
  default     = "15s"
}
