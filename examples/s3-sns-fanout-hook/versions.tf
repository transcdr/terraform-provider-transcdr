# -----------------------------------------------------------------------------
# Providers
# -----------------------------------------------------------------------------
#
#   hashicorp/aws      the bucket, the SNS topic and its subscription, and the
#                      IAM user Transcdr reads the bucket with.
#   transcdr/transcdr  the bucket connection and the hook automation.

terraform {
  # The examples are tested with Terraform 1.5 and newer.
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

# Credentials come from the environment (AWS_PROFILE, access keys, SSO or an
# instance role). They need to create S3 buckets, SNS topics and IAM users.
provider "aws" {
  region = var.region
}

# Reads TRANSCDR_API_KEY (and TRANSCDR_BASE_URL, if set). The key needs
# connections:write and automations:write; automations:write is also what lets
# Terraform read the automation's hook_url.
provider "transcdr" {}
