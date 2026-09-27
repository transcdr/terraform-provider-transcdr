package provider

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// A preset's output is written as a partial output specification (what differs from the defaults)
// and returned fully resolved. These helpers compare the two: the configuration is up to date when
// every value it sets has that value in the resolved spec.

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
	p := projectJSON(a, u)
	if reflect.DeepEqual(p, u) {
		return configured, true
	}
	b, err := json.Marshal(p)
	if err != nil {
		return string(api), false
	}
	return string(b), false
}

// removedJSONPaths lists the object keys, recursively, that before sets and after does not: values
// an update would leave in place, since the API merges output updates into the stored spec.
func removedJSONPaths(before, after string) []string {
	var b, a any
	if json.Unmarshal([]byte(before), &b) != nil || json.Unmarshal([]byte(after), &a) != nil {
		return nil
	}
	var out []string
	var walk func(b, a any, prefix []string)
	walk = func(b, a any, prefix []string) {
		bm, ok := b.(map[string]any)
		if !ok {
			return
		}
		am, ok := a.(map[string]any)
		if !ok {
			return
		}
		for k, bv := range bm {
			p := append(append([]string{}, prefix...), k)
			av, ok := am[k]
			if !ok {
				out = append(out, strings.Join(p, "."))
				continue
			}
			walk(bv, av, p)
		}
	}
	walk(b, a, nil)
	sort.Strings(out)
	return out
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
