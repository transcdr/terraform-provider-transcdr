# Terraform Provider for Transcdr

This provider lets you declare [Transcdr](https://transcdr.com) video pipelines as code:

- storage and messaging connections
- bucket automations (watch, hook and queue triggers)
- event destinations
- presets, including constant bit rate
- API keys

It composes with the AWS and Cloudflare providers, so you can wire bucket → queue or topic → Transcdr → destination end to end.

```hcl
data "transcdr_bucket_automation_setup" "this" {
  method    = "queue"
  bucket    = aws_s3_bucket.media.bucket
  prefix    = "incoming/"
  pattern   = "**/*.{mp4,mov}"
  queue_url = aws_sqs_queue.triggers.url
}

resource "aws_sqs_queue_policy" "triggers" {
  queue_url = aws_sqs_queue.triggers.url
  policy    = data.transcdr_bucket_automation_setup.this.queue_policy
}

resource "transcdr_automation" "ingest" {
  name                  = "Ingest"
  trigger               = "queue"
  trigger_connection_id = transcdr_connection.queue.id
  source = {
    connection_id = transcdr_connection.bucket.id
    prefix        = "incoming/"
  }
  preset = "hls-av1-abr"
}
```

| | |
|---|---|
| **Resources** | `transcdr_connection`, `transcdr_automation`, `transcdr_event_destination`, `transcdr_preset`, `transcdr_api_key` |
| **Data sources** | `transcdr_organization`, `transcdr_preset`, `transcdr_connection`, `transcdr_bucket_automation_setup` (local: IAM, queue and topic policies and S3 notification filters, identical to the dashboard's *Automate a bucket*) |
| **Docs** | [`docs/`](docs/index.md), in the Terraform Registry layout |
| **Examples** | [`examples/`](examples/README.md): which one to pick, how to run them, and how the pieces fit together. Complete pipelines: [`examples/s3-sqs-queue-automation`](examples/s3-sqs-queue-automation), [`examples/s3-sns-fanout-hook`](examples/s3-sns-fanout-hook), [`examples/r2-watch-folder`](examples/r2-watch-folder), [`examples/presets-and-destinations`](examples/presets-and-destinations) |

## Install

```hcl
terraform {
  required_providers {
    transcdr = {
      source  = "transcdr/transcdr"
      version = "~> 0.1"
    }
  }
}
```

The provider needs Terraform 1.5 or newer.

## Authentication

Create a secret API key in the dashboard (*Settings → API keys*) with the scopes of what you manage, then set it in the environment:

```sh
export TRANSCDR_API_KEY=tdk_live_…
# optional, e.g. for a local API:
export TRANSCDR_BASE_URL=http://localhost:8080
```

You can also set it in the provider block:

```hcl
provider "transcdr" {
  api_key  = var.transcdr_api_key   # sensitive
  base_url = "https://api.transcdr.com"
}
```

| Setting | Environment | Default |
|---|---|---|
| `api_key` | `TRANSCDR_API_KEY` | none (required) |
| `base_url` | `TRANSCDR_BASE_URL` | `https://api.transcdr.com` |
| `max_retries` | `TRANSCDR_MAX_RETRIES` | 4 |

Scopes per resource are listed in [docs/index.md](docs/index.md#authentication).

## Using a local build

Build the provider. Go is optional: the build runs in Docker.

```sh
# with Go
go build -o ~/terraform-providers/terraform-provider-transcdr .

# without Go: the binaries land in dist/
docker buildx build -f build/Dockerfile --target dist --build-arg TARGETS=linux/amd64 --output dist .
# windows/amd64 and darwin/arm64 also work as TARGETS
```

Point Terraform at the directory holding `terraform-provider-transcdr` (`.exe` on Windows) with `dev_overrides` in `~/.terraformrc` (`%APPDATA%\terraform.rc` on Windows):

```hcl
provider_installation {
  dev_overrides {
    "transcdr/transcdr" = "/home/you/terraform-providers"
  }
  # Everything else installs as usual.
  direct {}
}
```

With a dev override, skip `terraform init` for the Transcdr provider and run `terraform plan` or `apply` directly. A module that also uses other providers (AWS, Cloudflare) still needs `terraform init` for those. Current Terraform versions fail that init, because they try to install the overridden provider too. For such modules, use a filesystem mirror instead: copy the binary to `<mirror>/registry.terraform.io/transcdr/transcdr/0.1.0/<os>_<arch>/terraform-provider-transcdr_v0.1.0` and use this CLI config:

```hcl
provider_installation {
  filesystem_mirror {
    path    = "/path/to/mirror"
    include = ["transcdr/transcdr"]
  }
  direct {
    exclude = ["transcdr/transcdr"]
  }
}
```

## How it maps to the API

- **The Go SDK.** Every call goes through [transcdr-sdk-go](https://github.com/transcdr/transcdr-sdk-go), the public Go client for the API.
- **Retries.** Reads, deletes and PUTs that fail with 429 or 5xx, or cannot connect, are retried with exponential backoff and jitter, honouring `Retry-After` (`max_retries`). Creates and updates are sent once: the API deduplicates by `Idempotency-Key` only for jobs and uploads, so repeating another create could make a duplicate. Requests carry the user agent `terraform-provider-transcdr/<version> transcdr-sdk-go/<version>`.
- **Errors.** Errors show the API's message, every field error and the request id. A field error that names an attribute is attached to it.
- **Write-only secrets.** `transcdr_connection.secrets` and `transcdr_event_destination.aws.secret_access_key` are never returned. Terraform keeps the configured values and sends one again only when it changes, which is how you rotate it. If a secret Terraform set disappears from `secrets_set`, the next plan puts it back.
- **Rotations.**
  - Change `hook_token_version` to rotate an automation's `hook_url`.
  - Change `secret_version` to rotate an event destination's `signing_secret`.
  - Any change to an API key replaces it, with a new `secret`.
- **Drift.** Every resource reads the API on refresh. A connection or event destination the API turned off shows as `enabled` going back to `true`, and applying turns it back on. A preset's `output` compares only the fields you set against the resolved specification (`resolved_output`), so API defaults are not drift.

## Development

Go is optional. Everything runs through `build/Dockerfile` (golang:1.24):

```sh
docker buildx build -f build/Dockerfile --target test --progress=plain .   # gofmt check, go vet, unit tests
docker buildx build -f build/Dockerfile --target fmt  --output . .         # gofmt -w
docker buildx build -f build/Dockerfile --target tidy --output . .         # go mod tidy
docker buildx build -f build/Dockerfile --target docs --output . .         # regenerate docs/ (tfplugindocs)
```

Docs are generated from the schema descriptions, `templates/` and `examples/`. Don't edit `docs/` by hand.

### Acceptance tests

Acceptance tests (`TF_ACC=1`) create, update, import and destroy real objects. They need:

- a Transcdr API
- LocalStack, for the S3, SQS and SNS pieces
- `terraform` and Docker on the PATH

`scripts/testacc-up.sh` starts LocalStack (`localstack/localstack:4`). It also starts a local API from a checkout of the `transcdr` repository, with its own SQLite database, on port 18080, seeded with the demo organization and some credit:

```sh
TRANSCDR_REPO=../transcdr scripts/testacc-up.sh   # writes .testacc/env
scripts/testacc.sh                                # all TestAcc* tests
scripts/testacc.sh -run TestAccExample            # one test
scripts/testacc-down.sh                           # stops the API and LocalStack
```

`scripts/testacc.sh` uses `go test` when Go is installed. Otherwise it builds the test binary for this machine in Docker and runs it here.

`TestAccExample_s3SQSQueueAutomation` applies `examples/s3-sqs-queue-automation` as published, plus a LocalStack override file. It then uploads a file to the bucket and requires the automation to have turned the S3 notification on the queue into a job.

The generator golden files in `internal/setup/testdata` come from the dashboard's TypeScript. To regenerate them:

```sh
node internal/setup/testdata/gen_golden.mjs ../transcdr-frontend
```

## Releasing

Releases follow the Terraform Registry's format: zips for every platform, a `SHA256SUMS` file, its GPG signature, and the registry manifest. `.goreleaser.yml` builds them, and `.github/workflows/release.yml` runs it when a `v*` tag is pushed.

Signing is not configured yet. To enable it:

1. Create a GPG key for signing (RSA or DSA; the Registry does not accept ECC):
   ```sh
   gpg --full-generate-key
   gpg --armor --export-secret-keys <KEY_ID>
   ```
2. Add the public key (`gpg --armor --export <KEY_ID>`) to the `transcdr` namespace in the Terraform Registry (*User Settings → Signing Keys*).
3. Add the repository secrets `GPG_PRIVATE_KEY` (the armoured private key) and `PASSPHRASE`.
4. Push a tag such as `v0.1.0`. The workflow drafts a GitHub release. Publish it, and the Registry picks it up once the provider is published there.

The release workflow refuses to run without `GPG_PRIVATE_KEY`.
