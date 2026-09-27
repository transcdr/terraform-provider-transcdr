# -----------------------------------------------------------------------------
# Inputs
# -----------------------------------------------------------------------------

variable "webhook_url" {
  description = <<-EOT
    Where Transcdr POSTs events: a public https:// URL with a valid
    certificate that accepts JSON and answers 2xx within a few seconds (do
    the work after answering). Anything else is retried with backoff for up
    to a day. Hosts on private networks are refused.
  EOT
  type        = string
}

variable "secret_version" {
  description = <<-EOT
    Any number. Change it and apply to rotate the destination's signing
    secret: Transcdr issues a new one and the old one stops working at once,
    so update your receiver in the same change. Setting it on the first apply
    does not rotate anything.
  EOT
  type        = number
  default     = 1
}
