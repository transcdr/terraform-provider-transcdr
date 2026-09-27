terraform {
  required_version = ">= 1.5"
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
    transcdr = {
      source  = "transcdr/transcdr"
      version = "~> 0.1"
    }
  }
}

# Reads CLOUDFLARE_API_TOKEN: a token that can edit R2 buckets.
provider "cloudflare" {}

# Reads TRANSCDR_API_KEY (and TRANSCDR_BASE_URL, if set).
provider "transcdr" {}
