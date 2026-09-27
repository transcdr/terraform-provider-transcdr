# -----------------------------------------------------------------------------
# Providers
# -----------------------------------------------------------------------------
#
# This module uses three providers:
#
#   hashicorp/aws      the bucket, the queues, the SNS topic and the IAM user
#                      Transcdr signs in as.
#   hashicorp/time     one short wait, so brand-new IAM access keys work before
#                      Transcdr tries them (see time_sleep.iam in main.tf).
#   transcdr/transcdr  the connections, the preset, the automation and the
#                      event destination on the Transcdr side.
#
# Versions are pinned to a major release: `~> 6.0` accepts 6.x but not 7.0.

terraform {
  # The examples are tested with Terraform 1.5 and newer.
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    time = {
      source  = "hashicorp/time"
      version = "~> 0.12"
    }
    transcdr = {
      source  = "transcdr/transcdr"
      version = "~> 0.1"
    }
  }
}

# The AWS provider takes its credentials from the usual places: AWS_PROFILE,
# AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY, SSO, or an instance role. The
# credentials you run Terraform with need to create S3 buckets, SQS queues,
# SNS topics and IAM users; Transcdr itself gets a separate, much narrower
# IAM user created below.
provider "aws" {
  region = var.region
}

# The Transcdr provider reads TRANSCDR_API_KEY from the environment (and
# TRANSCDR_BASE_URL, if you point it at another API). Keeping the key out of
# the configuration keeps it out of version control; you can still set
# `api_key = var.transcdr_api_key` here if you prefer a sensitive variable.
#
# The key needs these scopes: connections:write, presets:write,
# automations:write and webhooks:write (or `*`).
provider "transcdr" {}
