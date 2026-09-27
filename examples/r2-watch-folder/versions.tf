# -----------------------------------------------------------------------------
# Providers
# -----------------------------------------------------------------------------
#
#   cloudflare/cloudflare  the R2 bucket.
#   transcdr/transcdr      the connection to the bucket and the watch
#                          automation.

terraform {
  # The examples are tested with Terraform 1.5 and newer.
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

# Reads CLOUDFLARE_API_TOKEN: an API token allowed to edit R2 buckets
# ("Workers R2 Storage: Edit"). This token only manages the bucket; Transcdr
# gets its own, narrower R2 credentials (see variables.tf).
provider "cloudflare" {}

# Reads TRANSCDR_API_KEY (and TRANSCDR_BASE_URL, if set). The key needs
# connections:write and automations:write.
provider "transcdr" {}
