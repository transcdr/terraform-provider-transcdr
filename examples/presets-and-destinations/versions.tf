terraform {
  required_version = ">= 1.5"
  required_providers {
    transcdr = {
      source  = "transcdr/transcdr"
      version = "~> 0.1"
    }
  }
}

# Reads TRANSCDR_API_KEY (and TRANSCDR_BASE_URL, if set).
provider "transcdr" {}
