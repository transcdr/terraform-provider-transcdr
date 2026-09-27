package provider

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
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

func TestProjectedOutput(t *testing.T) {
	api := json.RawMessage(`{"audio":{"mode":"auto"},"bit_depth":"auto","codec":"h264","color":"sdr","filters":null,"gop":null,
		"ladder":null,"max_fps":null,"mode":"hls","quality":{"bitrate":"3000k","buffer_ms":2000,"target":"cbr"},
		"renditions":[{"bitrate":"6M","height":1080,"width":1920},{"height":720,"width":1280}],"segment_seconds":4.0,"subtitles":null,"trim":null}`)

	// Everything configured matches: the configuration's own text comes back.
	configured := `{"mode":"hls","codec":"h264","segment_seconds":4,"quality":{"target":"cbr","bitrate":"3000k","buffer_ms":2000},
		"renditions":[{"width":1920,"height":1080,"bitrate":"6M"},{"width":1280,"height":720,"label":null}]}`
	if out, ok := projectedOutput(configured, api); !ok || out != configured {
		t.Fatalf("expected no drift, got %s", out)
	}

	// A changed value shows as drift on that field only.
	changed := `{"codec":"av1","quality":{"target":"cbr"}}`
	out, ok := projectedOutput(changed, api)
	if ok {
		t.Fatal("expected drift")
	}
	if !jsonEqual(out, `{"codec":"h264","quality":{"target":"cbr"}}`) {
		t.Fatalf("projection = %s", out)
	}

	// A rendition list of another length is shown whole.
	out, _ = projectedOutput(`{"renditions":[{"width":1920,"height":1080}]}`, api)
	if !jsonEqual(out, `{"renditions":[{"bitrate":"6M","height":1080,"width":1920},{"height":720,"width":1280}]}`) {
		t.Fatalf("projection = %s", out)
	}
}

func TestRemovedJSONPaths(t *testing.T) {
	got := removedJSONPaths(
		`{"codec":"h264","quality":{"target":"cbr","bitrate":"3M","buffer_ms":1000},"renditions":[{"width":1280,"height":720}],"gop":48}`,
		`{"codec":"av1","quality":{"target":"high"},"renditions":[{"width":1920,"height":1080}]}`,
	)
	want := []string{"gop", "quality.bitrate", "quality.buffer_ms"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("removed = %v, want %v", got, want)
	}
	if got := removedJSONPaths(`{"codec":"h264"}`, `{"codec":"av1","gop":48}`); len(got) != 0 {
		t.Fatalf("nothing removed, got %v", got)
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
