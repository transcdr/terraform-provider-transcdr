variable "cloudflare_account_id" {
  description = "The Cloudflare account that owns the bucket."
  type        = string
}

variable "bucket_name" {
  description = "The R2 bucket to create."
  type        = string
  default     = "transcdr-media"
}

variable "bucket_location" {
  description = "An R2 location hint: apac, eeur, enam, weur or wnam."
  type        = string
  default     = "enam"
}

variable "r2_access_key_id" {
  description = "The Access Key ID of an R2 API token with Object Read & Write on the bucket."
  type        = string
}

variable "r2_secret_access_key" {
  description = "The Secret Access Key of that token."
  type        = string
  sensitive   = true
}

variable "prefix" {
  description = "The folder Transcdr watches."
  type        = string
  default     = "incoming/"
}

variable "pattern" {
  description = "Only files matching this glob are transcoded."
  type        = string
  default     = "**/*.{mp4,mov,mkv,webm,m4v}"
}

variable "poll_interval_seconds" {
  description = "How often the folder is listed, 60 to 86400 seconds."
  type        = number
  default     = 300
}

variable "settle_seconds" {
  description = "A file is taken once it has been unchanged this long."
  type        = number
  default     = 60
}

variable "preset" {
  description = "A system preset slug or a preset id."
  type        = string
  default     = "hls-av1-abr"
}

variable "output_prefix" {
  description = "Where outputs go in the bucket. Keep it outside var.prefix, or outputs are taken again."
  type        = string
  default     = "transcoded/{date}/{stem}/"
}

variable "delete_source" {
  description = "Delete each source file once its job completes."
  type        = bool
  default     = false
}
