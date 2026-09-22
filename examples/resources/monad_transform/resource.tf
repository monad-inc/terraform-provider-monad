# A transform is an ordered list of operations (at most 20). Each operation
# names the operation and passes its arguments; the argument names are the ones
# on that operation's page in the Monad docs.
resource "monad_transform" "tag_and_trim" {
  name        = "Tag and trim"
  description = "Stamps the environment, then drops fields with no IR value"

  config = {
    operations = [
      {
        operation = "add"
        arguments = {
          key   = "environment"
          value = "production"
        }
      },
      {
        operation = "drop_key"
        arguments = { key = "requestID" }
      },
      {
        operation = "drop_key"
        arguments = { key = "responseElements.credentials.sessionToken" }
      },
    ]
  }
}
