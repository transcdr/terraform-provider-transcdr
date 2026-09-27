package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccHTTPSDestination(url, events, description string, enabled bool, extra string) string {
	return configHeader(false) + fmt.Sprintf(`
resource "transcdr_event_destination" "test" {
  url         = %q
  events      = %s
  description = %q
  enabled     = %t
  %s
}
`, url, events, description, enabled, extra)
}

func TestAccEventDestination_https(t *testing.T) {
	r := "transcdr_event_destination.test"
	var secret string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkGone("transcdr_event_destination", "/v1/webhooks/"),
		Steps: []resource.TestStep{
			{
				Config: testAccHTTPSDestination("https://example.com/hooks/transcdr", `["job.completed", "job.failed"]`, "tfacc", true, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(r, "id", regexp.MustCompile(`^whk_`)),
					resource.TestCheckResourceAttr(r, "type", "https"),
					resource.TestCheckResourceAttr(r, "target", "https://example.com/hooks/transcdr"),
					resource.TestCheckResourceAttr(r, "events.#", "2"),
					resource.TestCheckTypeSetElemAttr(r, "events.*", "job.completed"),
					resource.TestMatchResourceAttr(r, "signing_secret", regexp.MustCompile(`^whsec_`)),
					resource.TestCheckResourceAttrWith(r, "signing_secret", capture(&secret)),
				),
			},
			{
				ResourceName:            r,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"signing_secret"},
			},
			// Update in place; the secret stays.
			{
				Config: testAccHTTPSDestination("https://example.com/hooks/transcdr-v2", `["*"]`, "tfacc updated", false, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "url", "https://example.com/hooks/transcdr-v2"),
					resource.TestCheckResourceAttr(r, "events.#", "1"),
					resource.TestCheckResourceAttr(r, "description", "tfacc updated"),
					resource.TestCheckResourceAttr(r, "enabled", "false"),
					resource.TestCheckResourceAttrWith(r, "signing_secret", same(&secret)),
				),
			},
			// secret_version rotates the signing secret.
			{
				Config: testAccHTTPSDestination("https://example.com/hooks/transcdr-v2", `["*"]`, "tfacc updated", true, "secret_version = 2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(r, "signing_secret", regexp.MustCompile(`^whsec_`)),
					resource.TestCheckResourceAttrWith(r, "signing_secret", differs(&secret)),
				),
			},
		},
	})
}

func testAccAWSDestinations(suffix, secret string) string {
	return configHeader(true) + fmt.Sprintf(`
resource "aws_sns_topic" "t" {
  name = %[1]q
}

resource "aws_sqs_queue" "q" {
  name = %[1]q
}

resource "transcdr_event_destination" "sns" {
  topic_arn = aws_sns_topic.t.arn
  aws = {
    access_key_id     = %[4]q
    secret_access_key = %[5]q
    endpoint          = %[2]q
  }
  events = ["job.completed"]
}

resource "transcdr_event_destination" "sqs" {
  queue_url = %[3]q
  aws = {
    access_key_id     = %[4]q
    secret_access_key = %[5]q
    region            = "us-east-1"
  }
  depends_on = [aws_sqs_queue.q]
}

resource "transcdr_connection" "hook" {
  name   = "tfacc hook %[1]s"
  kind   = "webhook"
  config = { url = "https://example.com/hooks/%[1]s" }
}

resource "transcdr_event_destination" "via_connection" {
  connection_id = transcdr_connection.hook.id
  events        = ["job.failed", "connection.disabled"]
}
`, suffix, localstackURL(), queueURL(suffix), testAccessKeyID, secret)
}

func TestAccEventDestination_awsAndConnection(t *testing.T) {
	suffix := acctest.RandomWithPrefix("tfacc")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		ExternalProviders:        awsProvider,
		CheckDestroy:             checkGone("transcdr_event_destination", "/v1/webhooks/"),
		Steps: []resource.TestStep{
			{
				Config: testAccAWSDestinations(suffix, testSecretAccessKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("transcdr_event_destination.sns", "type", "sns"),
					resource.TestCheckResourceAttrPair("transcdr_event_destination.sns", "target", "aws_sns_topic.t", "arn"),
					// Read from the topic ARN: not configuration.
					resource.TestCheckNoResourceAttr("transcdr_event_destination.sns", "aws.region"),
					resource.TestCheckResourceAttr("transcdr_event_destination.sqs", "type", "sqs"),
					resource.TestCheckResourceAttr("transcdr_event_destination.sqs", "aws.region", "us-east-1"),
					resource.TestCheckResourceAttr("transcdr_event_destination.via_connection", "type", "https"),
					resource.TestCheckResourceAttr("transcdr_event_destination.via_connection", "target", "https://example.com/hooks/"+suffix),
					resource.TestCheckNoResourceAttr("transcdr_event_destination.via_connection", "url"),
					resource.TestMatchResourceAttr("transcdr_event_destination.via_connection", "signing_secret", regexp.MustCompile(`^whsec_`)),
				),
			},
			{
				ResourceName:      "transcdr_event_destination.sns",
				ImportState:       true,
				ImportStateVerify: true,
				// The API stores URLs normalised (http://localhost:4566/); an import has no other spelling.
				ImportStateVerifyIgnore: []string{"signing_secret", "aws.secret_access_key", "aws.endpoint"},
			},
			{
				ResourceName:            "transcdr_event_destination.sqs",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"signing_secret", "aws.secret_access_key"},
			},
			{
				ResourceName:            "transcdr_event_destination.via_connection",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"signing_secret"},
			},
			// Rotating the AWS secret updates in place.
			{
				Config: testAccAWSDestinations(suffix, testSecretAccessKey+"2"),
				Check:  resource.TestCheckResourceAttr("transcdr_event_destination.sns", "aws.secret_access_key", testSecretAccessKey+"2"),
			},
		},
	})
}

func TestAccEventDestination_validation(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: configHeader(false) + `
resource "transcdr_event_destination" "bad" {
  topic_arn = "arn:aws:sns:us-east-1:123456789012:t"
}
`,
				ExpectError: regexp.MustCompile(`needs the aws block`),
			},
			{
				Config: configHeader(false) + `
resource "transcdr_event_destination" "bad" {
  url       = "https://example.com"
  queue_url = "https://sqs.us-east-1.amazonaws.com/123456789012/q"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
		},
	})
}
