package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccAutomationBase is a LocalStack bucket and queue with their connections.
func testAccAutomationBase(suffix string) string {
	return configHeader(true) + fmt.Sprintf(`
resource "aws_s3_bucket" "b" {
  bucket        = %[1]q
  force_destroy = true
}

resource "aws_sqs_queue" "q" {
  name = %[1]q
}

resource "transcdr_connection" "bucket" {
  name = "tfacc bucket %[1]s"
  kind = "s3"
  config = {
    bucket     = aws_s3_bucket.b.bucket
    region     = "us-east-1"
    endpoint   = %[2]q
    path_style = true
  }
  secrets = {
    access_key_id     = %[4]q
    secret_access_key = %[5]q
  }
}

resource "transcdr_connection" "queue" {
  name = "tfacc queue %[1]s"
  kind = "sqs"
  config = {
    queue_url = %[3]q
    region    = "us-east-1"
  }
  secrets = {
    access_key_id     = %[4]q
    secret_access_key = %[5]q
  }
  depends_on = [aws_sqs_queue.q]
}
`, suffix, localstackURL(), queueURL(suffix), testAccessKeyID, testSecretAccessKey)
}

func TestAccAutomation(t *testing.T) {
	suffix := acctest.RandomWithPrefix("tfacc")
	r := "transcdr_automation.test"
	var hookURL string

	minimal := testAccAutomationBase(suffix) + fmt.Sprintf(`
resource "transcdr_automation" "test" {
  name = "tfacc %s"
  source = {
    connection_id = transcdr_connection.bucket.id
    prefix        = "incoming/"
  }
}
`, suffix)

	full := func(trigger, extra string) string {
		queue := ""
		if trigger == "queue" {
			queue = "trigger_connection_id = transcdr_connection.queue.id"
		}
		return testAccAutomationBase(suffix) + fmt.Sprintf(`
resource "transcdr_automation" "test" {
  name    = "tfacc %[1]s full"
  enabled = false
  trigger = %[2]q
  %[3]s

  source = {
    connection_id = transcdr_connection.bucket.id
    prefix        = "incoming/"
    pattern       = "**/*.{mp4,mov}"
  }
  poll_interval_seconds = 120
  settle_seconds        = 10

  preset = "hls-h264-abr"
  output = jsonencode({
    codec = "h264"
    renditions = [{ width = 1280, height = 720 }]
  })

  destination = {
    connection_id = transcdr_connection.bucket.id
    prefix        = "transcoded/{stem}/"
  }
  after_success = "delete"
  priority      = "high"
  metadata      = { team = "video" }
  webhook_url   = "https://example.com/jobs"
  %[4]s
}
`, suffix, trigger, queue, extra)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		ExternalProviders:        awsProvider,
		CheckDestroy:             checkGone("transcdr_automation", "/v1/automations/"),
		Steps: []resource.TestStep{
			{
				Config: minimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(r, "id", regexp.MustCompile(`^aut_`)),
					resource.TestCheckResourceAttr(r, "trigger", "watch"),
					resource.TestCheckResourceAttr(r, "enabled", "true"),
					resource.TestCheckResourceAttr(r, "poll_interval_seconds", "300"),
					resource.TestCheckResourceAttr(r, "settle_seconds", "60"),
					resource.TestCheckResourceAttr(r, "after_success", "keep"),
					resource.TestCheckResourceAttr(r, "priority", "normal"),
					// The API stores `incoming`; the configuration's spelling stays.
					resource.TestCheckResourceAttr(r, "source.prefix", "incoming/"),
					resource.TestCheckResourceAttr(r, "source.pattern", "**/*.{mp4,mov,mkv,webm,m4v,avi,ts,mts,m2ts,mxf}"),
					resource.TestCheckNoResourceAttr(r, "destination"),
					resource.TestCheckNoResourceAttr(r, "output"),
					resource.TestMatchResourceAttr(r, "hook_url", regexp.MustCompile(`/v1/hooks/automations/ahk_`)),
					resource.TestCheckResourceAttrWith(r, "hook_url", capture(&hookURL)),
				),
			},
			// Every field, and a queue trigger.
			{
				Config: full("queue", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "trigger", "queue"),
					resource.TestCheckResourceAttrPair(r, "trigger_connection_id", "transcdr_connection.queue", "id"),
					resource.TestCheckResourceAttr(r, "enabled", "false"),
					resource.TestCheckResourceAttr(r, "source.pattern", "**/*.{mp4,mov}"),
					resource.TestCheckResourceAttr(r, "poll_interval_seconds", "120"),
					resource.TestCheckResourceAttr(r, "settle_seconds", "10"),
					resource.TestCheckResourceAttr(r, "preset", "hls-h264-abr"),
					resource.TestCheckResourceAttr(r, "output", `{"codec":"h264","renditions":[{"height":720,"width":1280}]}`),
					resource.TestCheckResourceAttrPair(r, "destination.connection_id", "transcdr_connection.bucket", "id"),
					resource.TestCheckResourceAttr(r, "destination.prefix", "transcoded/{stem}/"),
					resource.TestCheckResourceAttr(r, "after_success", "delete"),
					resource.TestCheckResourceAttr(r, "priority", "high"),
					resource.TestCheckResourceAttr(r, "metadata.team", "video"),
					resource.TestCheckResourceAttr(r, "webhook_url", "https://example.com/jobs"),
					resource.TestCheckResourceAttrWith(r, "hook_url", same(&hookURL)),
				),
			},
			{
				ResourceName:      r,
				ImportState:       true,
				ImportStateVerify: true,
				// The API stores the prefix normalised (`incoming`); an import has no other spelling.
				ImportStateVerifyIgnore: []string{"source.prefix"},
			},
			// Hook trigger; changing hook_token_version rotates the hook URL.
			{
				Config: full("hook", "hook_token_version = 1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "trigger", "hook"),
					resource.TestCheckNoResourceAttr(r, "trigger_connection_id"),
					resource.TestCheckResourceAttrWith(r, "hook_url", differs(&hookURL)),
					resource.TestCheckResourceAttrWith(r, "hook_url", capture(&hookURL)),
				),
			},
			// Clearing optional fields: preset, output, metadata, webhook_url go back to none. Other
			// changes leave the hook URL alone.
			{
				Config: testAccAutomationBase(suffix) + fmt.Sprintf(`
resource "transcdr_automation" "test" {
  name               = "tfacc %s"
  trigger            = "hook"
  hook_token_version = 1
  source = {
    connection_id = transcdr_connection.bucket.id
    prefix        = "incoming/"
  }
  destination = {
    connection_id = transcdr_connection.bucket.id
  }
}
`, suffix),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(r, "preset"),
					resource.TestCheckNoResourceAttr(r, "output"),
					resource.TestCheckNoResourceAttr(r, "metadata.%"),
					resource.TestCheckNoResourceAttr(r, "webhook_url"),
					resource.TestCheckResourceAttr(r, "destination.prefix", "transcoded/{stem}/"),
					resource.TestCheckResourceAttrWith(r, "hook_url", same(&hookURL)),
				),
			},
			// The API cannot remove a destination: the plan says so instead of silently keeping it.
			{
				Config:      minimal,
				ExpectError: regexp.MustCompile(`cannot remove an automation's destination`),
			},
		},
	})
}

func TestAccAutomation_queueNeedsConnection(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: configHeader(false) + `
resource "transcdr_automation" "bad" {
  name    = "tfacc"
  trigger = "queue"
  source  = { connection_id = "con_0000000000000000000000" }
}
`,
				ExpectError: regexp.MustCompile(`needs the sqs connection to consume`),
			},
		},
	})
}
