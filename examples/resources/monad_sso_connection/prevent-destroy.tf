# Deleting an SSO connection deletes the users, roles and sessions bound to
# it. prevent_destroy makes Terraform refuse any plan that would delete it,
# including terraform destroy and removing the resource from configuration.
resource "monad_sso_connection" "okta" {
  name          = "okta"
  email_domains = ["example.com"]

  saml = {
    entity_id    = "0b6f1c3e-2d4a-4e8b-9f10-3c5d7e9a1b2c"
    metadata_url = "https://example.okta.com/app/exk1a2b3c4d5e6f7g8h9/sso/saml/metadata"
  }

  lifecycle {
    prevent_destroy = true
  }
}
