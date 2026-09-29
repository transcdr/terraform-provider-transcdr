package provider

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

func TestSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	var presp provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &presp)
	if presp.Diagnostics.HasError() {
		t.Fatalf("provider schema: %v", presp.Diagnostics)
	}
	if diags := presp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("provider schema: %v", diags)
	}

	names := map[string]bool{}
	for _, f := range p.Resources(ctx) {
		r := f()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "transcdr"}, &meta)
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("%s: %v", meta.TypeName, resp.Diagnostics)
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s: %v", meta.TypeName, diags)
		}
		if resp.Schema.MarkdownDescription == "" {
			t.Errorf("%s has no description", meta.TypeName)
		}
		for name, a := range resp.Schema.Attributes {
			if a.GetMarkdownDescription() == "" {
				t.Errorf("%s.%s has no description", meta.TypeName, name)
			}
		}
		names[meta.TypeName] = true
	}
	for _, f := range p.DataSources(ctx) {
		d := f()
		var meta datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "transcdr"}, &meta)
		var resp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &resp)
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s: %v", meta.TypeName, diags)
		}
		names["data."+meta.TypeName] = true
	}
	for _, want := range []string{
		"transcdr_connection", "transcdr_automation", "transcdr_event_destination", "transcdr_preset", "transcdr_api_key",
		"data.transcdr_organization", "data.transcdr_preset", "data.transcdr_connection", "data.transcdr_bucket_automation_setup",
	} {
		if !names[want] {
			t.Errorf("%s is not registered", want)
		}
	}
}

func TestSameURL(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"http://example.com", "http://example.com/", true},
		{"HTTPS://Example.COM/hooks", "https://example.com/hooks", true},
		{"http://localhost:4566", "http://localhost:4566/", true},
		{"https://example.com/a", "https://example.com/b", false},
		{"https://example.com/a?x=1", "https://example.com/a?x=2", false},
		{"https://example.com/Path", "https://example.com/path", false},
	} {
		if got := sameURL(c.a, c.b); got != c.want {
			t.Errorf("sameURL(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestSamePath(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"incoming/", "incoming", true},
		{"/incoming/", "incoming", true},
		{"a//b/./c/", "a/b/c", true},
		{"", "", true},
		{"incoming/", "", false},
		{"Incoming", "incoming", false},
	} {
		if got := samePath(c.a, c.b); got != c.want {
			t.Errorf("samePath(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// hlsSpec is a whole v2 output specification: the contract's HLS example.
const hlsSpec = `{"kind":"video","container":{"format":"hls","segment_seconds":6},
	"video":{"codec":"h264","cbr":{"bitrate":"standard","buffer_ms":1000},"bit_depth":"8bit","color":"sdr",
		"frame_rate":{"max":"source"},"gop":"segment","filters":[]},
	"audio":{"handling":"encode","codec":"aac","bitrate":"standard","channels":"source","he_aac":"auto","stereo_fallback":false},
	"renditions":{"sizes":[
		{"label":"by_size","width":1920,"height":1080,"fit":"contain","orientation":"auto","upscale":false,"video":{"cbr":{"bitrate":"5M"}}},
		{"label":"by_size","width":1280,"height":720,"fit":"contain","orientation":"auto","upscale":false}]},
	"subtitles":{"tracks":"all"},"trim":{"start":0,"end":"source"},"privacy":{"preset":"strip_all"}}`

// hlsResolved is hlsSpec as the API returns it: privacy written out as its four categories, and
// each size's effective rate.
var hlsResolved = json.RawMessage(`{"kind":"video","container":{"format":"hls","segment_seconds":6.0},
	"video":{"codec":"h264","cbr":{"bitrate":"standard","buffer_ms":1000},"bit_depth":"8bit","color":"sdr",
		"frame_rate":{"max":"source"},"gop":"segment","filters":[]},
	"audio":{"handling":"encode","codec":"aac","bitrate":"standard","channels":"source","he_aac":"auto","stereo_fallback":false},
	"renditions":{"sizes":[
		{"label":"by_size","width":1920,"height":1080,"fit":"contain","orientation":"auto","upscale":false,"video":{"cbr":{"bitrate":"5M"}}},
		{"label":"by_size","width":1280,"height":720,"fit":"contain","orientation":"auto","upscale":false,"video":{"cbr":{"bitrate":"standard"}}}]},
	"subtitles":{"tracks":"all"},"trim":{"start":0,"end":"source"},
	"privacy":{"location":"strip","capture_time":"strip","device":"strip","descriptive":"strip"}}`)

func TestProjectedOutput(t *testing.T) {
	// The whole spec as written matches its resolved form: a privacy preset matches the four
	// categories it stands for, and the rate the API writes on a size is not drift.
	if out, ok := projectedOutput(hlsSpec, hlsResolved); !ok || out != hlsSpec {
		t.Fatalf("expected no drift, got %s", out)
	}

	// A privacy preset refined by a field matches when the resolved categories agree.
	refined := strings.Replace(hlsSpec, `{"preset":"strip_all"}`, `{"preset":"strip_all","capture_time":"date"}`, 1)
	api := json.RawMessage(strings.Replace(string(hlsResolved), `"capture_time":"strip"`, `"capture_time":"date"`, 1))
	if out, ok := projectedOutput(refined, api); !ok || out != refined {
		t.Fatalf("expected no drift for a refined privacy preset, got %s", out)
	}
	// ... and is drift when they do not.
	if _, ok := projectedOutput(refined, hlsResolved); ok {
		t.Fatal("expected drift: capture_time is strip in the API")
	}

	// A changed value shows as drift on that field only.
	changed := `{"kind":"video","video":{"codec":"av1"}}`
	out, ok := projectedOutput(changed, hlsResolved)
	if ok {
		t.Fatal("expected drift")
	}
	if !jsonEqual(out, `{"kind":"video","video":{"codec":"h264"}}`) {
		t.Fatalf("projection = %s", out)
	}

	// A size list of another length is shown whole.
	out, _ = projectedOutput(`{"renditions":{"sizes":[{"width":1920}]}}`, hlsResolved)
	if !strings.Contains(out, `"height":720`) {
		t.Fatalf("projection = %s", out)
	}
}

func TestResolvedPrivacy(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`{"privacy":{"preset":"keep_all"}}`, `{"privacy":{"location":"keep","capture_time":"keep","device":"keep_all","descriptive":"keep"}}`},
		{`{"privacy":{"preset":"strip_location","device":"strip"}}`, `{"privacy":{"location":"strip","capture_time":"keep","device":"strip","descriptive":"keep"}}`},
		// Four fields, or a preset this provider does not know, are compared as written.
		{`{"privacy":{"location":"keep","capture_time":"keep","device":"keep","descriptive":"keep"}}`, `{"privacy":{"location":"keep","capture_time":"keep","device":"keep","descriptive":"keep"}}`},
		{`{"privacy":{"preset":"newer"}}`, `{"privacy":{"preset":"newer"}}`},
	} {
		var in any
		if err := json.Unmarshal([]byte(c.in), &in); err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(resolvedPrivacy(in))
		if !jsonEqual(string(got), c.want) {
			t.Errorf("resolvedPrivacy(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestCheckOutput(t *testing.T) {
	var diags diag.Diagnostics
	checkOutput(&diags, path.Root("output"), hlsSpec)
	if diags.HasError() {
		t.Fatalf("a whole spec: %v", diags)
	}

	// Every missing field is reported at once, with the API's messages.
	checkOutput(&diags, path.Root("output"), `{"kind":"audio","container":{"format":"mp3"},"audio":{"handling":"encode","codec":"mp3"}}`)
	var details []string
	for _, d := range diags.Errors() {
		details = append(details, d.Detail())
	}
	want := []string{
		"output.privacy is required: give privacy.preset (strip_all, strip_location or keep_all), or all of location, capture_time, device and descriptive.",
		"output.audio.bitrate is required when kind is video or audio and audio.handling is auto or encode and audio.codec is opus, mp3 or aac.",
		"output.audio.channels is required when kind is video or audio and audio.handling is auto or encode.",
		"output.audio.he_aac is required when kind is video or audio and audio.handling is auto or encode.",
	}
	if !reflect.DeepEqual(details, want) {
		t.Fatalf("details = %q", details)
	}

	// A v1 spec (no kind) is refused: the provider speaks v2.
	diags = nil
	checkOutput(&diags, path.Root("output"), `{"mode":"hls","codec":"h264"}`)
	if len(diags.Errors()) != 1 || !strings.Contains(diags.Errors()[0].Detail(), "output.kind is required") {
		t.Fatalf("v1: %v", diags)
	}
}

func TestAutomationOverridesKeepNull(t *testing.T) {
	// A null in the overrides removes the preset's field, so it must reach the API as null.
	m := automationModel{
		Name: types.StringValue("x"), Enabled: types.BoolValue(true), Trigger: types.StringValue("watch"),
		PollIntervalSeconds: types.Int64Value(300), SettleSeconds: types.Int64Value(60),
		AfterSuccess: types.StringValue("keep"), Priority: types.StringValue("normal"),
		Source:              &automationSourceModel{ConnectionID: types.StringValue("con_1"), Prefix: types.StringNull(), Pattern: types.StringNull()},
		Preset:              types.StringValue("hls-h264-abr@1"),
		Output:              jsontypes.NewNormalizedValue(`{"container":{"format":"mp4","segment_seconds":null},"video":{"gop":{"seconds":2}}}`),
		TriggerConnectionID: types.StringNull(), WebhookURL: types.StringNull(), Metadata: types.MapNull(types.StringType),
	}
	var diags diag.Diagnostics
	body, err := json.Marshal(m.params(context.Background(), nil, &diags))
	if err != nil || diags.HasError() {
		t.Fatal(err, diags)
	}
	var sent map[string]any
	_ = json.Unmarshal(body, &sent)
	got, _ := json.Marshal(sent["output"])
	if !jsonEqual(string(got), `{"container":{"format":"mp4","segment_seconds":null},"video":{"gop":{"seconds":2}}}`) {
		t.Fatalf("output sent = %s", got)
	}
	if sent["preset"] != "hls-h264-abr@1" {
		t.Fatalf("preset sent = %v", sent["preset"])
	}
}

func TestErrorDetailListsEveryProblem(t *testing.T) {
	e := &transcdr.Error{
		Status: 422, Type: "invalid_request_error", Code: "validation_failed",
		Param: "output.audio.bitrate", Message: "output.audio.bitrate is required.",
		Errors: []transcdr.FieldError{
			{Param: "output.audio.bitrate", Message: "output.audio.bitrate is required."},
			{Param: "output.privacy", Message: "output.privacy is required."},
		},
	}
	d := errorDetail(e)
	if !strings.Contains(d, "  - output.audio.bitrate: output.audio.bitrate is required.") || !strings.Contains(d, "  - output.privacy: output.privacy is required.") {
		t.Fatalf("detail = %s", d)
	}
}

func TestJSONEqual(t *testing.T) {
	if !jsonEqual(`{"a":1,"b":[1,2]}`, "{ \"b\": [1, 2],\n \"a\": 1.0 }") {
		t.Error("formatting, key order and 1 vs 1.0 must not matter")
	}
	if jsonEqual(`{"b":[1,2]}`, `{"b":[2,1]}`) {
		t.Error("array order matters")
	}
}

func TestConfigBodyClearsRemovedFields(t *testing.T) {
	prior := nullConfig()
	prior.Bucket = types.StringValue("media")
	prior.Root = types.StringValue("videos/")
	prior.PathStyle = types.BoolValue(true)
	prior.Endpoint = types.StringValue("https://acct.r2.cloudflarestorage.com")

	next := nullConfig()
	next.Bucket = types.StringValue("media")
	next.Region = types.StringValue("auto")

	body, _ := json.Marshal(configBody(&next, &prior))
	if !jsonEqual(string(body), `{"bucket":"media","region":"auto","root":null,"path_style":false,"endpoint":null}`) {
		t.Fatalf("body = %s", body)
	}
	body, _ = json.Marshal(configBody(&next, nil))
	if !jsonEqual(string(body), `{"bucket":"media","region":"auto"}`) {
		t.Fatalf("create body = %s", body)
	}
}

func TestSecretsBody(t *testing.T) {
	prior := &connectionSecrets{AccessKeyID: types.StringValue("AKIA1"), SecretAccessKey: types.StringValue("s1"), SessionToken: types.StringValue("t")}
	next := &connectionSecrets{AccessKeyID: types.StringValue("AKIA1"), SecretAccessKey: types.StringValue("s2"), SessionToken: types.StringNull()}
	got, _ := json.Marshal(secretsBody(next, prior))
	if !jsonEqual(string(got), `{"secret_access_key":"s2","session_token":""}`) {
		t.Fatalf("update = %s", got)
	}
	got, _ = json.Marshal(secretsBody(next, nil))
	if !jsonEqual(string(got), `{"access_key_id":"AKIA1","secret_access_key":"s2"}`) {
		t.Fatalf("create = %s", got)
	}
	if secretsBody(prior, prior) != nil {
		t.Fatal("nothing changed: no secrets are sent")
	}
}

func TestConnectionReadKeepsDerivedValuesOut(t *testing.T) {
	conn := &transcdr.Connection{
		ID: "con_1", Name: "q", Kind: "sqs", Class: "messaging", Status: "ok", Enabled: true,
		Config: transcdr.ConnectionConfig{
			QueueURL: transcdr.Value("https://sqs.eu-west-1.amazonaws.com/123456789012/q"),
			Region:   transcdr.Value("eu-west-1"),
		},
		SecretsSet: []string{"access_key_id"},
	}
	cfg := nullConfig()
	cfg.QueueURL = types.StringValue("https://sqs.eu-west-1.amazonaws.com/123456789012/q")
	prior := &connectionModel{
		Config:     &cfg,
		Secrets:    &connectionSecrets{AccessKeyID: types.StringValue("AKIA1"), SecretAccessKey: types.StringValue("s")},
		SecretsSet: stringSet([]string{"access_key_id", "secret_access_key"}),
	}
	var m connectionModel
	if diags := m.fromAPI(conn, prior, false); diags.HasError() {
		t.Fatal(diags)
	}
	if !m.Config.Region.IsNull() {
		t.Errorf("a region read from the queue URL is not configuration: %v", m.Config.Region)
	}
	if !m.Secrets.SecretAccessKey.IsNull() {
		t.Error("a secret the API no longer stores must be dropped so the next plan sends it")
	}
	if m.Secrets.AccessKeyID.ValueString() != "AKIA1" {
		t.Error("stored secrets stay as configured")
	}
}
