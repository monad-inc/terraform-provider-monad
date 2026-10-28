resource "monad_sso_connection" "okta" {
  name          = "okta"
  description   = "Corporate Okta"
  email_domains = ["example.com"]
  public_name   = "Example Corp SSO"

  saml = {
    # Enter this value in the identity provider as the audience (SP entity ID).
    entity_id    = "0b6f1c3e-2d4a-4e8b-9f10-3c5d7e9a1b2c"
    metadata_url = "https://example.okta.com/app/exk1a2b3c4d5e6f7g8h9/sso/saml/metadata"
  }
}
