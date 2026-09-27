variable "region" {
  description = "The AWS region of the bucket and the queue."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "A name for everything this creates. The bucket name gets a random suffix."
  type        = string
  default     = "transcdr-ingest"
}

variable "prefix" {
  description = "Only files uploaded under this prefix are transcoded."
  type        = string
  default     = "incoming/"
}

variable "pattern" {
  description = "Only files matching this glob are transcoded. A pattern ending in *.{a,b} becomes one S3 suffix filter per extension."
  type        = string
  default     = "**/*.{mp4,mov,mkv,webm,m4v}"
}

variable "preset" {
  description = "A system preset slug or a preset id."
  type        = string
  default     = "hls-av1-abr"
}

variable "output_prefix" {
  description = "Where outputs go in the same bucket: a template with {job_id}, {name}, {stem}, {ext}, {dir}, {date}, {automation}, {org}. Keep it outside var.prefix, or outputs are taken again."
  type        = string
  default     = "transcoded/{stem}/"
}

variable "notification_email" {
  description = "Optional: an email address subscribed to the completion topic."
  type        = string
  default     = null
}

variable "iam_propagation_wait" {
  description = "How long to wait for new IAM access keys to work before Transcdr tests them."
  type        = string
  default     = "15s"
}
