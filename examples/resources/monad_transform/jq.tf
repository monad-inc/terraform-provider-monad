# Keep long jq programs in their own file and load them with file(). An empty
# key replaces the record with the query result; a non-empty key stores the
# result under that key instead.
resource "monad_transform" "cloudtrail_to_ecs" {
  name        = "CloudTrail to ECS"
  description = "Normalizes CloudTrail into ECS v8.11.0"

  config = jsondecode(jsonencode({
    operations = [
      {
        operation = "jq"
        arguments = {
          key   = ""
          query = file("${path.module}/jq/cloudtrail-to-ecs.jq")
        }
      },
    ]
  }))
}
