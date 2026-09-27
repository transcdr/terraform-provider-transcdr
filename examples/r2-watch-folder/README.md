# Cloudflare R2 watch folder

This example creates an R2 bucket. Transcdr watches its `incoming/` folder, transcodes new videos, and writes the outputs back to the bucket under `transcoded/{date}/{stem}/`.

R2's event notifications do not reach SQS, so the automation uses `trigger = "watch"`. Transcdr lists the folder every `poll_interval_seconds` (default 300). It takes each file that has not changed for `settle_seconds` (default 60), and processes each version exactly once.

## Credentials

- **Cloudflare API token** (`CLOUDFLARE_API_TOKEN`), with permission to edit R2 buckets. The Cloudflare provider uses it to create the bucket.
- **R2 S3 credentials** for Transcdr. In the Cloudflare dashboard, open *R2 → Manage API tokens* and create an API token with *Object Read & Write* on the bucket. Pass its Access Key ID and Secret Access Key as `r2_access_key_id` and `r2_secret_access_key`. The token has to exist before Transcdr can use the bucket, and R2 shows its secret only once, so it is a variable here and not a resource.

The connection is an `s3` connection with R2's endpoint (`https://<account id>.r2.cloudflarestorage.com`) and region `auto`.

## Use it

```sh
export CLOUDFLARE_API_TOKEN=…
export TRANSCDR_API_KEY=tdk_live_…   # connections:write, automations:write
terraform init
terraform apply \
  -var cloudflare_account_id=… \
  -var r2_access_key_id=… \
  -var r2_secret_access_key=…
```

Upload a video to `incoming/`. Within `poll_interval_seconds` + `settle_seconds` a job starts. Set `delete_source = true` to remove each source file once its job completes.

`terraform output connection_status` shows the outcome of the connection test Transcdr ran when the connection was saved (`ok`, or `error` with `last_error` on the resource). If the keys stop working, Transcdr turns the connection off, and the next `terraform plan` shows `enabled` going back to `true`.
