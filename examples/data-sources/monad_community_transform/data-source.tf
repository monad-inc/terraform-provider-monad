# Use a published community transform as-is.
data "monad_community_transform" "okta_ocsf" {
  id = "ocsf:v1.9.0:okta:okta-systemlog"
}

resource "monad_transform" "okta_ocsf" {
  name        = data.monad_community_transform.okta_ocsf.name
  description = data.monad_community_transform.okta_ocsf.description
  config      = data.monad_community_transform.okta_ocsf.config
}
