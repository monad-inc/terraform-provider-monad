# Terraform Monad Provider

- Terraform: https://www.terraform.io
- Monad: https://beta.monad.com
- Community: [Join #monad on Slack →](https://join.slack.com/t/monad-community/shared_invite/zt-2l1xvgdv8-JqfJgqHfQFPqRBQO4TdoYQ)

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) 0.14.x
- [Go](https://golang.org/doc/install) 1.21 (to build the provider plugin)

## Building The Provider

Clone repository to: `$GOPATH/src/github.com/monad-inc/terraform-provider-monad`

```sh
$ export GOPATH=$(go env GOPATH)
$ mkdir -p $GOPATH/src/github.com/monad-inc; cd $GOPATH/src/github.com/monad-inc
$ git clone git@github.com:monad-inc/terraform-provider-monad
```

Enter the provider directory and build the provider

```sh
$ cd $GOPATH/src/github.com/monad-inc/terraform-provider-monad
$ go build -o terraform-provider-monad
```

## Using the provider

If you're building the provider, follow the instructions to [install it as a plugin.](https://www.terraform.io/docs/plugins/basics.html#installing-a-plugin) After placing it into your plugins directory, run `terraform init` to initialize it.

Further [usage documentation is available on the Terraform website](https://registry.terraform.io/providers/monad-inc/monad/latest/docs).

## Developing the Provider

If you wish to work on the provider, you'll first need [Go](http://www.golang.org) installed on your machine (version 1.21+ is _required_). You'll also need to correctly setup a [GOPATH](http://golang.org/doc/code.html#GOPATH), as well as adding `$GOPATH/bin` to your `$PATH`.

To compile the provider, run `go build`. This will build the provider and put the provider binary in the current directory.

```sh
$ go build -o terraform-provider-monad
```

In order to test the provider, you can simply run `go test`.

```sh
$ go test ./...
```

### Development with Local Provider

To use the provider locally:

1. Build the provider: `go build -o terraform-provider-monad`
2. Create a `.terraformrc` file in your home directory:

```hcl
provider_installation {
  dev_overrides {
    "monad-inc/monad" = "/path/to/your/terraform-provider-monad"
  }

  direct {}
}
```

3. Use the provider in your Terraform configurations

### Using Task Runner

This project includes a Taskfile for common development tasks:

```sh
# Build the provider
$ task build

# Generate documentation
$ task generate

# Apply example configuration
$ task example-apply

# Destroy example configuration
$ task example-destroy
```

## Provider Configuration

```hcl
terraform {
  required_providers {
    monad = {
      source = "monad-inc/monad"
    }
  }
}

provider "monad" {
  base_url        = "https://beta.monad.com"  # Optional, defaults to this value
  api_token       = var.monad_api_token       # Can use MONAD_API_TOKEN env var
  organization_id = var.organization_id       # Can use MONAD_ORGANIZATION_ID env var
}
```

## Environment Variables

- `MONAD_BASE_URL` - Base URL for the Monad API
- `MONAD_API_TOKEN` - API token for authentication
- `MONAD_ORGANIZATION_ID` - Organization ID for all resources

## Resources

### monad_secret

Manages organization secrets that can be referenced by other resources.

- `name` (string, required) - Name of the secret
- `value` (string, required, sensitive) - Secret value
- `reference` (string, computed) - Reference string to use in other resources

### monad_pipeline

Manages data pipelines that connect inputs to outputs with conditional logic.

- `name` (string, required) - Name of the pipeline
- `description` (string, optional) - Description of the pipeline
- `nodes` (list, required) - Pipeline nodes configuration
- `edges` (list, required) - Pipeline edge connections

### monad_input

Generic input connector for data sources.

- `name` (string, required) - Name of the input
- `description` (string, optional) - Description of the input
- `type` (string, required) - Type of input connector (e.g., "demo", "okta_systemlog")
- `config` (block, optional) - Input configuration

### monad_output

Generic output connector for data destinations.

- `name` (string, required) - Name of the output
- `description` (string, optional) - Description of the output
- `type` (string, required) - Type of output connector (e.g., "http", "postgresql")
- `config` (block, optional) - Output configuration

### monad_transform

Generic transform connector for data transformations.

- `name` (string, required) - Name of the transform
- `description` (string, optional) - Description of the transform
- `type` (string, required) - Type of transform connector
- `config` (block, optional) - Transform configuration

## Import

All resources support import using their respective resource IDs:

```bash
# Import secrets
terraform import monad_secret.example secret-id-here

# Import pipelines
terraform import monad_pipeline.example pipeline-id-here

# Import inputs
terraform import monad_input.example input-id-here

# Import outputs
terraform import monad_output.example output-id-here

# Import transforms
terraform import monad_transform.example transform-id-here
```
