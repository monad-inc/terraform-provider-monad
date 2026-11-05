data "monad_permissions" "all" {}

# Every read permission, for an auditor role.
resource "monad_role" "auditor" {
  name        = "Auditor"
  permissions = [for s in data.monad_permissions.all.slugs : s if endswith(s, ":read")]
}
