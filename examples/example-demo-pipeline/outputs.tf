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