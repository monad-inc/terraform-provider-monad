# A read-only role for dashboards and audits. Permissions are slugs; see the
# monad_permissions data source for the full catalog.
resource "monad_role" "pipeline_viewer" {
  name        = "Pipeline viewer"
  description = "Read pipelines and their components"
  permissions = [
    "pipeline:read",
    "input:read",
    "output:read",
    "transform:read",
  ]
}

# A least-privilege key that acts as the role.
resource "monad_api_key" "dashboards" {
  name            = "dashboards"
  role_id         = monad_role.pipeline_viewer.id
  expiration_time = "2027-01-01T00:00:00Z"
}
