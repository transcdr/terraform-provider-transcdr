data "transcdr_organization" "this" {}

# Connections and automations need the integrations feature.
locals {
  can_automate = contains(data.transcdr_organization.this.features, "integrations")
}
