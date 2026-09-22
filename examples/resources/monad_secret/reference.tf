# Reference the secret's id wherever a component expects a credential. The
# reference is also what stops Terraform from destroying the secret while a
# component still uses it: Monad refuses to delete a referenced secret, and the
# dependency makes Terraform remove the component first.
resource "monad_output" "archive" {
  name = "Archive (S3)"
  type = "s3"

  config {
    settings = {
      bucket = var.archive_bucket
      region = "us-west-2"
      prefix = "archive"
    }

    secrets = {
      access_key = { id = monad_secret.archive_access_key.id }
      secret_key = { id = monad_secret.archive_secret_key.id }
    }
  }
}
