variable "webhook_url" {
  description = "A public https URL that accepts POSTed JSON and answers 2xx quickly."
  type        = string
}

variable "secret_version" {
  description = "Change to rotate the event destination's signing secret."
  type        = number
  default     = 1
}
