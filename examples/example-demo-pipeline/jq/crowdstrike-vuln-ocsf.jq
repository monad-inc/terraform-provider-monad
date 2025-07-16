# Helper function to safely convert timestamp
def safe_timestamp:
  if . and (. | type) == "string" then (. | fromdateiso8601) else null end;

# Helper function to convert Crowdstrike severity to OCSF severity_id
def severity_to_id:
  if . == "CRITICAL" then 5
  elif . == "HIGH" then 4 
  elif . == "MEDIUM" then 3
  elif . == "LOW" then 2
  else 99  # Unknown
  end;

# Helper function to convert status to OCSF status_id
def status_to_id:
  if . == "open" then 1
  elif . == "closed" then 2
  elif . == "in_progress" then 3
  else 99  # Unknown
  end;

# Main transformation
{
  # Required OCSF Fields
  "activity_id": 1,  # Detection
  "category_uid": 2, # Findings
  "class_uid": 2002, # Vulnerability Finding class
  "type_uid": 200201, # Vulnerability Finding type

  # Timestamps
  "time": (.created_timestamp | safe_timestamp),

  # Severity mapping
  "severity_id": ((.cve.severity // "UNKNOWN") | severity_to_id),
  "severity": (.cve.severity // "UNKNOWN"),

  # Finding Info
  "finding_info": {
    "title": (.vulnerability_id // "UNKNOWN"),
    "uid": (.id // "UNKNOWN"),
    "desc": (.cve.description // "No description available"),
    "created_time": (.created_timestamp | safe_timestamp),
    "modified_time": (.updated_timestamp | safe_timestamp)
  },

  # Vulnerabilities array
  "vulnerabilities": [
    {
      "title": (.vulnerability_id // "UNKNOWN"),
      "desc": (.cve.description // "No description available"),
      "severity": (.cve.severity // "UNKNOWN"),
      "cve": {
        "uid": (.vulnerability_id // "UNKNOWN"),
        "desc": (.cve.description // "No description available"),
        "cvss": [
          {
            "version": "3.1",
            "base_score": (.cve.base_score // 0),
            "vector_string": (.cve.vector // "UNKNOWN")
          }
        ],
        "references": (.cve.references // []),
        "modified_time": (.updated_timestamp | safe_timestamp)
      },
      "remediation": {
        "desc": (
          if .remediation and .remediation.entities then
            (.remediation.entities | map(.action) | join("; "))
          else 
            ""
          end
        ),
        "references": (
          if .remediation and .remediation.entities then
            (.remediation.entities | map(.link) | map(select(length > 0)))
          else 
            []
          end
        ),
        "kb_articles": (
          if .remediation and .remediation.entities then
            (.remediation.entities | map(.reference) | map(select(length > 0)))
          else 
            []
          end
        )
      },
      "first_seen_time": (.created_timestamp | safe_timestamp),
      "last_seen_time": (.updated_timestamp | safe_timestamp),
      "is_exploit_available": false
    }
  ],

  # Instead of using tostring, lets include a simplified version of raw data
  "raw_data": {
    "id": .id,
    "vulnerability_id": .vulnerability_id,
    "status": .status,
    "timestamp": .created_timestamp
  },

  # Metadata
  "metadata": {
    "version": "1.1.0",
    "product": {
      "name": "Crowdstrike Spotlight",
      "vendor_name": "Crowdstrike",
      "version": "1.0"
    },
    "profiles": ["vulnerability"],
    "uid": (.id // "UNKNOWN")
  },

  # Optional Fields
  "confidence": (.confidence // "UNKNOWN"),
  "confidence_id": (
    if .confidence == "confirmed" then 2
    elif .confidence == "high" then 2
    elif .confidence == "medium" then 1
    elif .confidence == "low" then 0
    else 99  # Unknown
    end
  ),

  "status": (.status // "unknown"),
  "status_id": ((.status // "unknown") | status_to_id),

  # Resource Details
  "resource": {
    "type": "host",
    "name": (.host_info.hostname // "unknown"),
    "uid": (.aid // "unknown"),
    "criticality": (.host_info.asset_criticality // "Unassigned"),
    "group": {
      "name": (
        if .host_info.groups and (.host_info.groups | type) == "array" then
          (.host_info.groups | join(", "))
        else 
          ""
        end
      )
    },
    "labels": [
      (.host_info.os_version // "unknown"),
      (.host_info.platform // "unknown"),
      (.host_info.product_type_desc // "unknown")
    ]
  },

  # Cloud context if available
  "cloud": (
    if .host_info and .host_info.service_provider then {
      "provider": (.host_info.service_provider // "unknown"),
      "account_uid": (.host_info.service_provider_account_id // "unknown"),
      "instance_uid": (.host_info.instance_id // "unknown")
    } else null
    end
  )
}