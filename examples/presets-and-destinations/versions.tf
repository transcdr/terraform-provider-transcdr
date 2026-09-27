# -----------------------------------------------------------------------------
# Providers
# -----------------------------------------------------------------------------
#
# Only the Transcdr provider: presets and event destinations live entirely in
# Transcdr, so no cloud account is needed.

terraform {
  # The examples are tested with Terraform 1.5 and newer.
  required_version = ">= 1.5"

  required_providers {
    transcdr = {
      source  = "transcdr/transcdr"
      version = "~> 0.1"
    }
  }
}

# Reads TRANSCDR_API_KEY (and TRANSCDR_BASE_URL, if set). The key needs
# presets:write, webhooks:write and org:read.
#
# A test-mode key (tdk_test_…) works too: presets and destinations behave the
# same, and jobs run with a test key complete with placeholder outputs for
# free, which is a cheap way to try a preset's settings end to end.
provider "transcdr" {}
