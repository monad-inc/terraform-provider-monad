resource "monad_pipeline" "example-pipeline" {
  name        = "Example: Crowdstrike Vulnerabilities to OCSF"
  description = "Demo pipeline to show how to transform CrowdStrike Vulnerability data into OCSF"

  nodes {
    slug           = "input"
    component_type = "input"
    component_id   = monad_input.input.id
  }

  nodes {
    slug           = "ingested-timestamp-transform"
    component_type = "transform"
    component_id   = monad_transform.ingested-timestamp-transform.id
  }

  nodes {
    slug           = "crowdstrike-vuln-ocsf-transform"
    component_type = "transform"
    component_id   = monad_transform.crowdstrike-vuln-ocsf-transform.id
  }

  nodes {
    slug           = "production-tag-transform"
    component_type = "transform"
    component_id   = monad_transform.production-tag-transform.id
  }

  nodes {
    slug           = "archive-output"
    component_type = "output"
    component_id   = monad_output.archive-output.id
  }

  nodes {
    slug           = "splunk-output"
    component_type = "output"
    component_id   = monad_output.splunk-output.id
  }

  nodes {
    slug           = "sec-lake-output"
    component_type = "output"
    component_id   = monad_output.sec-lake-output.id
  }

  edges {
    from_node_instance_slug = "input"
    to_node_instance_slug   = "ingested-timestamp-transform"
    condition {
      operator = "always"
    }
  }

  edges {
    from_node_instance_slug = "input"
    to_node_instance_slug   = "crowdstrike-vuln-ocsf-transform"
    condition {
      operator = "always"
    }
  }

  edges {
    from_node_instance_slug = "ingested-timestamp-transform"
    to_node_instance_slug   = "production-tag-transform"
    condition {
      operator = "always"
    }
  }

  edges {
    from_node_instance_slug = "production-tag-transform"
    to_node_instance_slug   = "archive-output"
    condition {
      operator = "always"
    }
  }

  edges {
    from_node_instance_slug = "crowdstrike-vuln-ocsf-transform"
    to_node_instance_slug   = "sec-lake-output"
    condition {
      operator = "always"
    }
  }

  edges {
    name                    = "only send high severity vulns"
    from_node_instance_slug = "crowdstrike-vuln-ocsf-transform"
    to_node_instance_slug   = "splunk-output"

    condition {
      operator = "and"
      conditions {
        type_id = "key_values"
        config {
          key   = "severity"
          value = ["HIGH"]
        }
      }
    }
  }
}

output "pipeline_id" {
  value = monad_pipeline.example-pipeline.id
}