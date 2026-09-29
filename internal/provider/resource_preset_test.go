package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

// testAccHLSSpec is a whole v2 spec: an H.264 HLS ladder at constant bit rate.
func testAccHLSSpec(bitrate string, bufferMS int) string {
	return fmt.Sprintf(`{
    kind      = "video"
    container = { format = "hls", segment_seconds = 4 }
    video = {
      codec      = "h264"
      cbr        = { bitrate = %q, buffer_ms = %d }
      bit_depth  = "8bit"
      color      = "sdr"
      frame_rate = { max = "source" }
      gop        = "segment"
      filters    = []
    }
    audio = { handling = "encode", codec = "aac", bitrate = "standard", channels = "source", he_aac = "auto", stereo_fallback = false }
    renditions = {
      sizes = [
        { label = "by_size", width = 1920, height = 1080, fit = "contain", orientation = "auto", upscale = false, video = { cbr = { bitrate = "6M" } } },
        { label = "by_size", width = 1280, height = 720, fit = "contain", orientation = "auto", upscale = false },
      ]
    }
    subtitles = { tracks = "all" }
    trim      = { start = 0, end = "source" }
    privacy   = { preset = "strip_all" }
  }`, bitrate, bufferMS)
}

func testAccCBRPreset(name, description, spec string) string {
	return configHeader(false) + fmt.Sprintf(`
resource "transcdr_preset" "test" {
  name        = %q
  description = %q
  output      = jsonencode(%s)
  metadata    = { tier = "broadcast" }
}
`, name, description, spec)
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
				Config: testAccCBRPreset(name, "Constant bit rate", testAccHLSSpec("4M", 1500)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(r, "id", regexp.MustCompile(`^pre_`)),
					resource.TestCheckResourceAttr(r, "slug", name),
					resource.TestCheckResourceAttr(r, "version", "1"),
					resource.TestCheckResourceAttr(r, "metadata.tier", "broadcast"),
					// The resolved spec writes privacy out; output stays as written.
					resource.TestMatchResourceAttr(r, "resolved_output", regexp.MustCompile(`"cbr":\{"bitrate":"4M","buffer_ms":1500\}`)),
					resource.TestMatchResourceAttr(r, "resolved_output", regexp.MustCompile(`"descriptive":"strip"`)),
					resource.TestCheckResourceAttrWith(r, "id", capture(&id)),
				),
			},
			// Refreshing finds no drift: the privacy preset matches its four categories.
			{
				Config:   testAccCBRPreset(name, "Constant bit rate", testAccHLSSpec("4M", 1500)),
				PlanOnly: true,
			},
			{
				ResourceName:      r,
				ImportState:       true,
				ImportStateVerify: true,
				// An import has only the resolved spec to go on.
				ImportStateVerifyIgnore: []string{"output"},
			},
			// A changed value updates in place, as a new version.
			{
				Config: testAccCBRPreset(name, "Constant bit rate, 5M", testAccHLSSpec("5M", 1500)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(r, "id", same(&id)),
					resource.TestCheckResourceAttr(r, "version", "2"),
					resource.TestCheckResourceAttr(r, "description", "Constant bit rate, 5M"),
					resource.TestMatchResourceAttr(r, "resolved_output", regexp.MustCompile(`"bitrate":"5M"`)),
				),
			},
			// Drift: the preset changed outside Terraform.
			{
				PreConfig: func() {
					id := stateID(t, "transcdr_preset", name)
					out := transcdr.OutputOverrides{"video": map[string]any{"cbr": map[string]any{"bitrate": "9M"}}}
					if _, err := testClient().Presets.Update(context.Background(), id, &transcdr.PresetUpdateParams{Output: out}); err != nil {
						t.Fatal(err)
					}
				},
				Config:             testAccCBRPreset(name, "Constant bit rate, 5M", testAccHLSSpec("5M", 1500)),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying puts the configuration back, as a new version.
			{
				Config: testAccCBRPreset(name, "Constant bit rate, 5M", testAccHLSSpec("5M", 1500)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(r, "id", same(&id)),
					resource.TestCheckResourceAttr(r, "version", "4"),
					checkAPI("/v1/presets/", r, func(obj map[string]any) error {
						cbr := obj["output"].(map[string]any)["video"].(map[string]any)["cbr"].(map[string]any)
						if cbr["bitrate"] != "5M" {
							return fmt.Errorf("bitrate not restored: %v", cbr)
						}
						return nil
					}),
				),
			},
			// Removing the description and the metadata clears them in place.
			{
				Config: configHeader(false) + fmt.Sprintf(`
resource "transcdr_preset" "test" {
  name   = %q
  output = jsonencode(%s)
}
`, name, testAccHLSSpec("5M", 1500)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(r, "id", same(&id)),
					resource.TestCheckResourceAttr(r, "description", ""),
					resource.TestCheckNoResourceAttr(r, "metadata.%"),
					resource.TestCheckResourceAttr(r, "slug", name),
					resource.TestCheckResourceAttr(r, "version", "4"),
					checkAPI("/v1/presets/", r, func(obj map[string]any) error {
						if obj["description"] != "" || len(obj["metadata"].(map[string]any)) != 0 {
							return fmt.Errorf("not replaced: description %v, metadata %v", obj["description"], obj["metadata"])
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
				// An incomplete spec is refused at plan time, every missing field named.
				Config: configHeader(false) + `
resource "transcdr_preset" "bad" {
  name   = "tfacc invalid"
  output = jsonencode({ kind = "audio", container = { format = "mp3" }, audio = { handling = "encode", codec = "mp3" } })
}
`,
				ExpectError: regexp.MustCompile(`(?s)Incomplete output specification.*output.privacy is required.*output.audio.bitrate is required`),
			},
			{
				// A complete spec the API refuses (HDR in 8-bit): the API's message comes through.
				Config: configHeader(false) + fmt.Sprintf(`
resource "transcdr_preset" "bad" {
  name   = "tfacc invalid"
  output = jsonencode(%s)
}
`, strings.Replace(testAccHLSSpec("5M", 1000), `color      = "sdr"`, `color      = "hdr10"`, 1)),
				ExpectError: regexp.MustCompile(`(?s)Could not create the preset.*HTTP (422|403)`),
			},
		},
	})
}
