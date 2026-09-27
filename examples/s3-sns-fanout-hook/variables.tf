variable "region" {
  description = "The AWS region of the bucket and the topic."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "A name for everything this creates. The bucket name gets a random suffix."
  type        = string
  default     = "transcdr-hook"
}

variable "prefix" {
  description = "Only files uploaded under this prefix are transcoded."
  type        = string
  default     = "incoming/"
}

variable "pattern" {
  description = "Only files matching this glob are transcoded."
  type        = string
  default     = "**/*.{mp4,mov}"
}

variable "preset" {
  description = "A system preset slug or a preset id."
  type        = string
  default     = "hls-h264-abr"
}

variable "hook_token_version" {
  description = "Change to rotate the automation's hook URL (the SNS subscription is replaced with it)."
  type        = number
  default     = 1
}
