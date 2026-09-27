terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    transcdr = {
      source  = "transcdr/transcdr"
      version = "~> 0.1"
    }
  }
}

provider "aws" {
  region = var.region
}

# Reads TRANSCDR_API_KEY (and TRANSCDR_BASE_URL, if set).
provider "transcdr" {}
