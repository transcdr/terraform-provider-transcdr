terraform {
  required_providers {
    transcdr = {
      source  = "transcdr/transcdr"
      version = "~> 0.1"
    }
  }
}

# Every setting is optional. With none, the provider reads TRANSCDR_API_KEY
# (and TRANSCDR_BASE_URL) from the environment, which keeps the key out of
# your configuration and version control.
provider "transcdr" {
  # A secret API key, tdk_live_… (or tdk_test_… for test mode, where jobs
  # complete with placeholder outputs for free). Give it the scopes of what
  # this configuration manages, e.g. connections:write, automations:write,
  # webhooks:write, presets:write.
  api_key = var.transcdr_api_key

  # Only for a different API, such as a local development server:
  # base_url = "http://localhost:8080"

  # Retries for reads and deletes that fail with 429 or 5xx (default 4).
  # max_retries = 4
}

variable "transcdr_api_key" {
  type      = string
  sensitive = true # never printed in plans or logs
}
