# Some connector types take no settings at all. Omit the config block entirely.
resource "monad_input" "http" {
  name        = "HTTP ingest"
  description = "Records POSTed to the pipeline's ingest endpoint"
  type        = "monad-http"
}
