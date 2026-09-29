package provider

import (
	"encoding/json"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

// A preset's output is written as a whole output specification (v2: every field its kind needs)
// and returned resolved: the same values, with privacy written out as its four categories and each
// size's effective constant rate filled in. These helpers compare the two: the configuration is up
// to date when every value it sets has that value in the resolved spec.

// privacyPresets are what each privacy preset stands for.
var privacyPresets = map[string]map[string]any{
	"strip_all":      {"location": "strip", "capture_time": "strip", "device": "strip", "descriptive": "strip"},
	"strip_location": {"location": "strip", "capture_time": "keep", "device": "keep", "descriptive": "keep"},
	"keep_all":       {"location": "keep", "capture_time": "keep", "device": "keep_all", "descriptive": "keep"},
}

// resolvedPrivacy is user with privacy as the API returns it: a preset written out as its four
// categories, the fields given beside it over them. Anything else is returned as it is.
func resolvedPrivacy(user any) any {
	doc, ok := user.(map[string]any)
	if !ok {
		return user
	}
	privacy, ok := doc["privacy"].(map[string]any)
	if !ok {
		return user
	}
	name, ok := privacy["preset"].(string)
	base, known := privacyPresets[name]
	if !ok || !known {
		return user
	}
	resolved := make(map[string]any, 4)
	for k, v := range base {
		resolved[k] = v
	}
	for k, v := range privacy {
		if k != "preset" {
			resolved[k] = v
		}
	}
	out := make(map[string]any, len(doc))
	for k, v := range doc {
		out[k] = v
	}
	out["privacy"] = resolved
	return out
}

// checkOutput adds a diagnostic on attr for every problem ValidateOutputJSON finds in a whole
// output specification: every missing field at once, as the API's 422 would list them.
func checkOutput(diags *diag.Diagnostics, attr path.Path, output string) {
	for _, fe := range transcdr.ValidateOutputJSON(json.RawMessage(output)) {
		diags.AddAttributeError(attr, "Incomplete output specification", fe.Message)
	}
}

// projectJSON keeps only the parts of api that user names: object keys recursively, and arrays of
// the same length element by element. What user does not mention is dropped, so the result can be
// compared with user directly.
func projectJSON(api, user any) any {
	switch u := user.(type) {
	case map[string]any:
		a, ok := api.(map[string]any)
		if !ok {
			return api
		}
		out := make(map[string]any, len(u))
		for k, uv := range u {
			if av, ok := a[k]; ok {
				out[k] = projectJSON(av, uv)
			} else if uv == nil {
				// The API leaves out fields that are absent: null is the same.
				out[k] = nil
			}
		}
		return out
	case []any:
		a, ok := api.([]any)
		if !ok || len(a) != len(u) {
			return api
		}
		out := make([]any, len(a))
		for i := range a {
			out[i] = projectJSON(a[i], u[i])
		}
		return out
	default:
		return api
	}
}

// projectedOutput is the configured output as the API has it now: the configuration's own text
// when nothing differs, else the projection (which Terraform shows as drift).
func projectedOutput(configured string, api json.RawMessage) (string, bool) {
	var u, a any
	if json.Unmarshal([]byte(configured), &u) != nil || json.Unmarshal(api, &a) != nil {
		return string(api), false
	}
	want := resolvedPrivacy(u)
	p := projectJSON(a, want)
	if reflect.DeepEqual(p, want) {
		return configured, true
	}
	b, err := json.Marshal(p)
	if err != nil {
		return string(api), false
	}
	return string(b), false
}

// compactJSON re-encodes raw compactly, or returns it as it is.
func compactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}
