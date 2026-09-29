package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccDataSources(t *testing.T) {
	suffix := acctest.RandomWithPrefix("tfacc")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: configHeader(false) + fmt.Sprintf(`
data "transcdr_organization" "this" {}

data "transcdr_preset" "system" {
  slug = "hls-av1-abr"
}

resource "transcdr_connection" "hook" {
  name   = "tfacc lookup %[1]s"
  kind   = "webhook"
  config = { url = "https://example.com/%[1]s" }
}

data "transcdr_connection" "by_name" {
  name = transcdr_connection.hook.name
}

data "transcdr_connection" "by_id" {
  id = transcdr_connection.hook.id
}

resource "transcdr_preset" "own" {
  name = "tfacc lookup %[1]s"
  # A system preset's spec with another codec: output spec v2 nests it in video.
  output = jsonencode(merge(jsondecode(data.transcdr_preset.system.output), {
    video = merge(jsondecode(data.transcdr_preset.system.output).video, { codec = "h264" })
  }))
}

data "transcdr_preset" "own" {
  id = transcdr_preset.own.id
}

data "transcdr_preset" "pinned" {
  slug    = "hls-av1-abr"
  version = 1
}
`, suffix),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("data.transcdr_organization.this", "id", regexp.MustCompile(`^org_`)),
					resource.TestCheckTypeSetElemAttr("data.transcdr_organization.this", "features.*", "integrations"),
					resource.TestCheckTypeSetElemAttr("data.transcdr_organization.this", "features.*", "webhooks"),
					resource.TestCheckResourceAttrSet("data.transcdr_organization.this", "max_resolution"),

					resource.TestCheckResourceAttr("data.transcdr_preset.system", "id", "hls-av1-abr"),
					resource.TestCheckResourceAttr("data.transcdr_preset.system", "system", "true"),
					resource.TestMatchResourceAttr("data.transcdr_preset.system", "output", regexp.MustCompile(`"format":"hls"`)),
					resource.TestCheckResourceAttrSet("data.transcdr_preset.system", "version"),
					resource.TestCheckResourceAttr("data.transcdr_preset.pinned", "version", "1"),
					resource.TestMatchResourceAttr("data.transcdr_preset.pinned", "output", regexp.MustCompile(`"kind":"video"`)),

					resource.TestCheckResourceAttrPair("data.transcdr_connection.by_name", "id", "transcdr_connection.hook", "id"),
					resource.TestCheckResourceAttr("data.transcdr_connection.by_name", "kind", "webhook"),
					resource.TestCheckResourceAttr("data.transcdr_connection.by_name", "config.url", "https://example.com/"+suffix),
					resource.TestCheckResourceAttrPair("data.transcdr_connection.by_id", "name", "transcdr_connection.hook", "name"),

					resource.TestCheckResourceAttr("data.transcdr_preset.own", "system", "false"),
					resource.TestMatchResourceAttr("data.transcdr_preset.own", "output", regexp.MustCompile(`"codec":"h264"`)),
					resource.TestCheckResourceAttr("data.transcdr_preset.own", "version", "1"),
				),
			},
		},
	})
}

func TestAccBucketAutomationSetup(t *testing.T) {
	d := "data.transcdr_bucket_automation_setup."
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: configHeader(false) + `
data "transcdr_bucket_automation_setup" "queue" {
  method            = "queue"
  bucket            = "media-in"
  region            = "us-east-1"
  root              = "videos"
  prefix            = "incoming/"
  pattern           = "**/*.{mp4,mov}"
  queue_url         = "https://sqs.us-east-1.amazonaws.com/123456789012/transcdr"
  outputs_to_bucket = true
}

data "transcdr_bucket_automation_setup" "fanout" {
  method    = "queue"
  fanout    = "sns"
  bucket    = "media-in"
  queue_url = "https://sqs.us-east-1.amazonaws.com/123456789012/transcdr"
  topic_arn = "arn:aws:sns:us-east-1:123456789012:media-uploads"
  pattern   = "**/*"
}

data "transcdr_bucket_automation_setup" "watch" {
  method = "watch"
  bucket = "media-in"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(d+"queue", "trigger", "queue"),
					resource.TestCheckResourceAttr(d+"queue", "key_prefix", "videos/incoming/"),
					resource.TestCheckResourceAttr(d+"queue", "resolved_queue_arn", "arn:aws:sqs:us-east-1:123456789012:transcdr"),
					resource.TestCheckResourceAttr(d+"queue", "queue_policy",
						`{"Version":"2012-10-17","Statement":[{"Sid":"S3SendsObjectEvents","Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Action":"sqs:SendMessage","Resource":"arn:aws:sqs:us-east-1:123456789012:transcdr","Condition":{"ArnLike":{"aws:SourceArn":"arn:aws:s3:::media-in"},"StringEquals":{"aws:SourceAccount":"123456789012"}}}]}`),
					resource.TestMatchResourceAttr(d+"queue", "iam_policy", regexp.MustCompile(`"s3:PutObject".*"ConsumeTranscdrTriggers"`)),
					resource.TestCheckResourceAttr(d+"queue", "notification_filters.#", "2"),
					resource.TestCheckResourceAttr(d+"queue", "notification_filters.0.id", "transcdr-mp4"),
					resource.TestCheckResourceAttr(d+"queue", "notification_filters.0.filter_prefix", "videos/incoming/"),
					resource.TestCheckResourceAttr(d+"queue", "notification_filters.1.filter_suffix", ".mov"),
					resource.TestCheckResourceAttr(d+"queue", "suffix_filters.#", "2"),
					resource.TestCheckNoResourceAttr(d+"queue", "topic_policy"),

					resource.TestMatchResourceAttr(d+"fanout", "queue_policy", regexp.MustCompile(`"SnsFansOutObjectEvents"`)),
					resource.TestMatchResourceAttr(d+"fanout", "topic_policy", regexp.MustCompile(`"S3PublishesObjectEvents"`)),
					resource.TestMatchResourceAttr(d+"fanout", "bucket_notification", regexp.MustCompile(`^\{"TopicConfigurations"`)),
					resource.TestCheckResourceAttr(d+"fanout", "by_suffix", "false"),
					resource.TestCheckNoResourceAttr(d+"fanout", "notification_filters.0.filter_suffix"),

					resource.TestCheckResourceAttr(d+"watch", "trigger", "watch"),
					resource.TestCheckResourceAttr(d+"watch", "roles.#", "2"),
					resource.TestCheckNoResourceAttr(d+"watch", "queue_policy"),
					resource.TestCheckNoResourceAttr(d+"watch", "bucket_notification"),
				),
			},
		},
	})
}
