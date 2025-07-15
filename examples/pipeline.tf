terraform {
  required_providers {
    monad = {
      source = "monad-inc/monad"
    }
  }
}

variable "api_token" {
  description = "Monad API token"
  type        = string
  sensitive   = true
}

variable "organization_id" {
  description = "Organization ID for all resources"
  type        = string
}

provider "monad" {
  base_url        = "https://localhost"
  api_token       = var.api_token
  organization_id = var.organization_id
}

resource "monad_input" "input" {
  name        = "Crowdstrike Vulnerabilities (Event Generator Example)"
  description = "This input uses synthetic data events to mimic CrowdStrike vulnerability data and provide data to test against"
  type        = "demo"

  config {
    settings = {
      record_type = "crowdstrike_vulnerabilities"
      rate        = 1
    }
  }
}

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
          query = "# Helper function to safely convert timestamp\ndef safe_timestamp:\n  if . and (. | type) == \"string\" then (. | fromdateiso8601) else null end;\n\n# Helper function to convert Crowdstrike severity to OCSF severity_id\ndef severity_to_id:\n  if . == \"CRITICAL\" then 5\n  elif . == \"HIGH\" then 4 \n  elif . == \"MEDIUM\" then 3\n  elif . == \"LOW\" then 2\n  else 99  # Unknown\n  end;\n\n# Helper function to convert status to OCSF status_id\ndef status_to_id:\n  if . == \"open\" then 1\n  elif . == \"closed\" then 2\n  elif . == \"in_progress\" then 3\n  else 99  # Unknown\n  end;\n\n# Main transformation\n{\n  # Required OCSF Fields\n  \"activity_id\": 1,  # Detection\n  \"category_uid\": 2, # Findings\n  \"class_uid\": 2002, # Vulnerability Finding class\n  \"type_uid\": 200201, # Vulnerability Finding type\n\n  # Timestamps\n  \"time\": (.created_timestamp | safe_timestamp),\n\n  # Severity mapping\n  \"severity_id\": ((.cve.severity // \"UNKNOWN\") | severity_to_id),\n  \"severity\": (.cve.severity // \"UNKNOWN\"),\n\n  # Finding Info\n  \"finding_info\": {\n    \"title\": (.vulnerability_id // \"UNKNOWN\"),\n    \"uid\": (.id // \"UNKNOWN\"),\n    \"desc\": (.cve.description // \"No description available\"),\n    \"created_time\": (.created_timestamp | safe_timestamp),\n    \"modified_time\": (.updated_timestamp | safe_timestamp)\n  },\n\n  # Vulnerabilities array\n  \"vulnerabilities\": [\n    {\n      \"title\": (.vulnerability_id // \"UNKNOWN\"),\n      \"desc\": (.cve.description // \"No description available\"),\n      \"severity\": (.cve.severity // \"UNKNOWN\"),\n      \"cve\": {\n        \"uid\": (.vulnerability_id // \"UNKNOWN\"),\n        \"desc\": (.cve.description // \"No description available\"),\n        \"cvss\": [\n          {\n            \"version\": \"3.1\",\n            \"base_score\": (.cve.base_score // 0),\n            \"vector_string\": (.cve.vector // \"UNKNOWN\")\n          }\n        ],\n        \"references\": (.cve.references // []),\n        \"modified_time\": (.updated_timestamp | safe_timestamp)\n      },\n      \"remediation\": {\n        \"desc\": (\n          if .remediation and .remediation.entities then\n            (.remediation.entities | map(.action) | join(\"; \"))\n          else \n            \"\"\n          end\n        ),\n        \"references\": (\n          if .remediation and .remediation.entities then\n            (.remediation.entities | map(.link) | map(select(length \u003e 0)))\n          else \n            []\n          end\n        ),\n        \"kb_articles\": (\n          if .remediation and .remediation.entities then\n            (.remediation.entities | map(.reference) | map(select(length \u003e 0)))\n          else \n            []\n          end\n        )\n      },\n      \"first_seen_time\": (.created_timestamp | safe_timestamp),\n      \"last_seen_time\": (.updated_timestamp | safe_timestamp),\n      \"is_exploit_available\": false\n    }\n  ],\n\n  # Instead of using tostring, lets include a simplified version of raw data\n  \"raw_data\": {\n    \"id\": .id,\n    \"vulnerability_id\": .vulnerability_id,\n    \"status\": .status,\n    \"timestamp\": .created_timestamp\n  },\n\n  # Metadata\n  \"metadata\": {\n    \"version\": \"1.1.0\",\n    \"product\": {\n      \"name\": \"Crowdstrike Spotlight\",\n      \"vendor_name\": \"Crowdstrike\",\n      \"version\": \"1.0\"\n    },\n    \"profiles\": [\"vulnerability\"],\n    \"uid\": (.id // \"UNKNOWN\")\n  },\n\n  # Optional Fields\n  \"confidence\": (.confidence // \"UNKNOWN\"),\n  \"confidence_id\": (\n    if .confidence == \"confirmed\" then 2\n    elif .confidence == \"high\" then 2\n    elif .confidence == \"medium\" then 1\n    elif .confidence == \"low\" then 0\n    else 99  # Unknown\n    end\n  ),\n\n  \"status\": (.status // \"unknown\"),\n  \"status_id\": ((.status // \"unknown\") | status_to_id),\n\n  # Resource Details\n  \"resource\": {\n    \"type\": \"host\",\n    \"name\": (.host_info.hostname // \"unknown\"),\n    \"uid\": (.aid // \"unknown\"),\n    \"criticality\": (.host_info.asset_criticality // \"Unassigned\"),\n    \"group\": {\n      \"name\": (\n        if .host_info.groups and (.host_info.groups | type) == \"array\" then\n          (.host_info.groups | join(\", \"))\n        else \n          \"\"\n        end\n      )\n    },\n    \"labels\": [\n      (.host_info.os_version // \"unknown\"),\n      (.host_info.platform // \"unknown\"),\n      (.host_info.product_type_desc // \"unknown\")\n    ]\n  },\n\n  # Cloud context if available\n  \"cloud\": (\n    if .host_info and .host_info.service_provider then {\n      \"provider\": (.host_info.service_provider // \"unknown\"),\n      \"account_uid\": (.host_info.service_provider_account_id // \"unknown\"),\n      \"instance_uid\": (.host_info.instance_id // \"unknown\")\n    } else null\n    end\n  )\n}\n"
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
resource "monad_output" "archive-output" {
  name        = "AWS S3 Archive Example (/dev/null)"
  description = "This example output archives data into S3 to preserve it for incident response before it is modified by the pipeline"
  type        = "dev-null"
}

resource "monad_output" "splunk-output" {
  name        = "Splunk Example (/dev/null)"
  description = "This example output mimics sending to Splunk, but discards the data instead for example purposes"
  type        = "dev-null"
}

resource "monad_output" "sec-lake-output" {
  name        = "AWS Security Lake Example (/dev/null)"
  description = "This example output mimics sending to aws security lake, but discards the data instead for example purposes"
  type        = "dev-null"
}

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
    name                    =  "only send high severity vulns"
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