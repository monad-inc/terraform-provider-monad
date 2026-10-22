# Import an API key by its ID. The API never returns a key's token after
# creation, so `token` is null in state until the key is next regenerated.
terraform import monad_api_key.example 37989e32-7c9f-48a8-bdae-eb84d45a79ee
