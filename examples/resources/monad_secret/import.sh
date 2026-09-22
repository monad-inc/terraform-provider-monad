# Import a secret by its ID. Monad never returns the value, so the first plan
# after import re-sends the configured value once and records its fingerprint.
terraform import monad_secret.example 3f1c9e64-8a12-4c7d-9f30-1b2c4d5e6f70
