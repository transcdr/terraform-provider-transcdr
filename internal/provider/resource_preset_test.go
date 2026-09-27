package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

func testAccCBRPreset(name, description, quality string) string {
	return configHeader(false) + fmt.Sprintf(`
resource "transcdr_preset" "test" {
  name        = %q
  description = %q
  output = jsonencode({
    mode            = "hls"
    codec           = "h264"
    segment_seconds = 4
    quality         = %s
    renditions = [
      { width = 1920, height = 1080, bitrate = "6M" },
      { width = 1280, height = 720 },
    ]
  })
  metadata = { tier = "broadcast" }
}
`, name, description, quality)
}

func TestAccPreset_cbr(t *testing.T) {
	name := acctest.RandomWithPrefix("tfacc")
	r := "transcdr_preset.test"
	var id string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkGone("transcdr_preset", "/v1/presets/"),
		Steps: []resource.TestStep{
			{
				Config: testAccCBRPreset(name, "Constant bit rate", `{ target = "cbr", bitrate = "4M", buffer_ms = 1500 }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(r, "id", regexp.MustCompile(`^pre_`)),
					resource.TestCheckResourceAttr(r, "slug", name),
					resource.TestCheckResourceAttr(r, "metadata.tier", "broadcast"),
					// The resolved spec has the defaults filled in; output stays as written.
					resource.TestMatchResourceAttr(r, "resolved_output", regexp.MustCompile(`"target":"cbr"`)),
					resource.TestMatchResourceAttr(r, "resolved_output", regexp.MustCompile(`"color":"sdr"`)),
					resource.TestCheckResourceAttrWith(r, "id", capture(&id)),
				),
			},
			{
				ResourceName:      r,
				ImportState:       true,
				ImportStateVerify: true,
				// An import has only the resolved spec to go on.
				ImportStateVerifyIgnore: []string{"output"},
			},
			// A changed value updates in place.
			{
				Config: testAccCBRPreset(name, "Constant bit rate, 5M", `{ target = "cbr", bitrate = "5M", buffer_ms = 1500 }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(r, "id", same(&id)),
					resource.TestCheckResourceAttr(r, "description", "Constant bit rate, 5M"),
					resource.TestMatchResourceAttr(r, "resolved_output", regexp.MustCompile(`"bitrate":"5M"`)),
				),
			},
			// Drift: the preset changed outside Terraform.
			{
				PreConfig: func() {
					id := stateID(t, "transcdr_preset", name)
					out := &transcdr.OutputSpecInput{Quality: &transcdr.Quality{Bitrate: transcdr.String("9M")}}
					if _, err := testClient().Presets.Update(context.Background(), id, &transcdr.PresetUpdateParams{Output: out}); err != nil {
						t.Fatal(err)
					}
				},
				Config:             testAccCBRPreset(name, "Constant bit rate, 5M", `{ target = "cbr", bitrate = "5M", buffer_ms = 1500 }`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// A removed field goes back to its default, in place: the whole preset is sent (PUT).
			{
				Config: testAccCBRPreset(name, "Constant bit rate, 5M", `{ target = "cbr", bitrate = "5M" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(r, "id", same(&id)),
					resource.TestMatchResourceAttr(r, "resolved_output", regexp.MustCompile(`"bitrate":"5M"`)),
					resource.TestCheckResourceAttr(r, "slug", name),
					checkAPI("/v1/presets/", r, func(obj map[string]any) error {
						q, _ := obj["output"].(map[string]any)["quality"].(map[string]any)
						if q["buffer_ms"] == float64(1500) {
							return fmt.Errorf("buffer_ms kept its old value: %v", q)
						}
						return nil
					}),
				),
			},
			// Removing the description, the metadata and whole output fields clears them in place.
			{
				Config: configHeader(false) + fmt.Sprintf(`
resource "transcdr_preset" "test" {
  name   = %q
  output = jsonencode({ mode = "hls", codec = "h264" })
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(r, "id", same(&id)),
					resource.TestCheckResourceAttr(r, "description", ""),
					resource.TestCheckNoResourceAttr(r, "metadata.%"),
					resource.TestCheckResourceAttr(r, "slug", name),
					checkAPI("/v1/presets/", r, func(obj map[string]any) error {
						out := obj["output"].(map[string]any)
						q, _ := out["quality"].(map[string]any)
						if q["target"] == "cbr" || obj["description"] != "" || len(obj["metadata"].(map[string]any)) != 0 {
							return fmt.Errorf("not replaced: description %v, metadata %v, quality %v", obj["description"], obj["metadata"], q)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestAccPreset_invalidSpec(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				// A rate without constant bit rate is refused; the API's message comes through.
				Config: configHeader(false) + `
resource "transcdr_preset" "bad" {
  name   = "tfacc invalid"
  output = jsonencode({ quality = { target = "high", bitrate = "5M" } })
}
`,
				ExpectError: regexp.MustCompile(`(?s)Could not create the preset.*HTTP 422`),
			},
		},
	})
}
