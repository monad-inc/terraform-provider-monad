# The default provider manages the sub-organization in the parent org.
provider "monad" {
  organization_id = var.parent_organization_id
}

# An aliased provider points at the new sub-organization. The same API key
# works if its role permits managing these resources in sub-organizations.
provider "monad" {
  alias           = "acme"
  organization_id = monad_organization.acme.id
}

resource "monad_organization" "acme" {
  name = "acme"
}

resource "monad_secret" "acme_token" {
  provider = monad.acme
  name     = "acme-token"
  value    = var.acme_token
}
