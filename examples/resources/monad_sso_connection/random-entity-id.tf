# random_uuid keeps the same value for the life of the resource, including
# when the connection is replaced, so the identity provider's audience
# setting never has to change.
resource "random_uuid" "sso_entity_id" {}

resource "monad_sso_connection" "okta" {
  name          = "okta"
  email_domains = ["example.com"]

  saml = {
    entity_id    = random_uuid.sso_entity_id.result
    metadata_url = var.okta_metadata_url
  }
}

output "sso_entity_id" {
  value = monad_sso_connection.okta.saml.entity_id
}
