resource "monad_transform" "ingested-timestamp-transform" {
  name        = "Add ingested timestamp"
  description = "adds a timestamp to show when the record was processed"

  config {
    operations = [
      {
        operation = "timestamp"
        arguments = {
          format = "RFC3339"
          key    = "ingested_timestamp"
        }
      }
    ]
  }
}

resource "monad_transform" "crowdstrike-vuln-ocsf-transform" {
  name        = "OCSF CrowdStrike Vulnerability Findings"
  description = "Transforms CrowdStrike Vulnerability Findings data into OCSF Schema"

  config {
    operations = [
      {
        operation = "jq"
        arguments = {
          query = file("${path.module}/crowdstrike-vuln-ocsf.jq")
        }
      }
    ]
  }
}

resource "monad_transform" "production-tag-transform" {
  name        = "Add production tag"
  description = "Adds the production tag to the record"

  config {
    operations = [
      {
        operation = "add"
        arguments = {
          key    = "environment_tag"
          value  = "production"
        }
      }
    ]
  }
}