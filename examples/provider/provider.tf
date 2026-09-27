terraform {
  required_providers {
    transcdr = {
      source  = "transcdr/transcdr"
      version = "~> 0.1"
    }
  }
}

# Both settings are optional: they default to the TRANSCDR_API_KEY and
# TRANSCDR_BASE_URL environment variables.
provider "transcdr" {
  api_key = var.transcdr_api_key
}

variable "transcdr_api_key" {
  type      = string
  sensitive = true
}
