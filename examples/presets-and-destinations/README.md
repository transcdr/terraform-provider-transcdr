# Presets and an HTTPS event destination

This example manages presets and an HTTPS event destination:

- `transcdr_preset.cbr_broadcast` is H.264 HLS at a **constant bit rate** (`quality.target = "cbr"`). Each rendition is coded at its own `bitrate`, or at `quality.bitrate` when it has none, within a 1.5 s buffer (`buffer_ms`). This suits players, networks and broadcast chains that need predictable bandwidth. HLS `BANDWIDTH` states each rung's rate.
- `transcdr_preset.web_h265_abr` is the system preset `hls-h264-abr` switched to H.265. It is read with the `transcdr_preset` data source and merged in HCL.
- `transcdr_preset.archive` is single-file AV1 at a VMAF target, 10-bit, with the source's colour passed through.
- `transcdr_event_destination.https` receives `job.completed`, `job.failed`, `job.delivery_failed` and `connection.disabled` as signed POSTs.
- `data.transcdr_organization.this` gives the organization's plan.

## Use it

```sh
export TRANSCDR_API_KEY=tdk_live_…   # presets:write, webhooks:write, org:read
terraform init
terraform apply -var webhook_url=https://example.com/hooks/transcdr
terraform output -raw signing_secret
```

Use the preset ids from `terraform output presets` as a job's or automation's `preset`.

## How preset outputs are compared

You write only the fields you want. The API fills in the rest, and `resolved_output` shows the full specification. Terraform compares only the fields you set, so API defaults never show up as drift, but a field changed outside Terraform does. Every change updates the preset in place, keeping its id: Terraform sends the whole preset (`PUT`), so a field you remove goes back to its default.

## Verifying deliveries

Every POST carries `Transcdr-Signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<body>")>`. To rotate the secret, change `secret_version` and apply. The old secret stops working at once.
