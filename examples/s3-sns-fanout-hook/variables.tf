# -----------------------------------------------------------------------------
# Inputs
# -----------------------------------------------------------------------------
#
# Every variable has a working default.

variable "region" {
  description = "The AWS region of the bucket and the topic. S3 can only notify a topic in the bucket's own region."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "A name for everything this creates. The bucket name gets a random suffix, since bucket names are global."
  type        = string
  default     = "transcdr-hook"
}

variable "prefix" {
  description = "Only files uploaded under this prefix are transcoded. It becomes the S3 notification's prefix filter."
  type        = string
  default     = "incoming/"
}

variable "pattern" {
  description = <<-EOT
    Only files matching this glob are transcoded (`*`, `**`, `?`, `{a,b}`;
    case-insensitive in Transcdr). A pattern ending in *.{mp4,mov} becomes
    one S3 notification per extension; S3 suffix filters are case-sensitive.
  EOT
  type        = string
  default     = "**/*.{mp4,mov}"
}

variable "preset" {
  description = "How each file is transcoded: a system preset slug (e.g. hls-h264-abr) or one of your preset ids."
  type        = string
  default     = "hls-h264-abr"
}

variable "hook_token_version" {
  description = <<-EOT
    Any number. The automation's hook_url is a credential: anyone who has it
    can make Transcdr process files. Change this number and apply to rotate
    it; Transcdr issues a new URL, the old one stops working, and Terraform
    replaces the SNS subscription so it points at the new URL.
  EOT
  type        = number
  default     = 1
}
