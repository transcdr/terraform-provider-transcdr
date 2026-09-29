package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

// clientFrom unpacks the provider data handed to Configure of a resource or data source.
func clientFrom(data any, diags *diag.Diagnostics) *transcdr.Client {
	if data == nil {
		return nil
	}
	pd, ok := data.(*providerData)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("Expected *providerData, got %T. Please report this issue to the provider developers.", data))
		return nil
	}
	if !pd.hasKey {
		diags.AddError("Missing Transcdr API key",
			"Set api_key in the provider block, or the TRANSCDR_API_KEY environment variable, to a secret API key (tdk_live_… or tdk_test_…).")
		return nil
	}
	return pd.client
}

// addAPIError turns an error from the client into a diagnostic. An API error carries its message,
// every field error and the request id; a field error whose param names an attribute is attached
// to that attribute too.
func addAPIError(diags *diag.Diagnostics, summary string, err error, paramPath func(string) (path.Path, bool)) {
	apiErr, ok := transcdr.AsError(err)
	if !ok {
		diags.AddError(summary, err.Error())
		return
	}
	if paramPath != nil && apiErr.Param != "" {
		if p, ok := paramPath(apiErr.Param); ok {
			diags.AddAttributeError(p, summary, errorDetail(apiErr))
			return
		}
	}
	diags.AddError(summary, errorDetail(apiErr))
}

// errorDetail is an API error's explanation for a diagnostic: the message, each field error, and
// the request id to quote to support.
func errorDetail(e *transcdr.Error) string {
	var b strings.Builder
	b.WriteString(e.Message)
	if len(e.Details) > 0 {
		b.WriteString("\n\nField errors:")
		keys := make([]string, 0, len(e.Details))
		for k := range e.Details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, field := range keys {
			for _, msg := range e.Details[field] {
				fmt.Fprintf(&b, "\n  - %s: %s", field, msg)
			}
		}
	} else if len(e.Errors) > 1 {
		// An output spec refused: every problem, in the API's order.
		b.WriteString("\n\nEvery problem:")
		for _, fe := range e.Errors {
			fmt.Fprintf(&b, "\n  - %s: %s", fe.Param, fe.Message)
		}
	} else if e.Param != "" {
		fmt.Fprintf(&b, "\n\nField: %s", e.Param)
	}
	fmt.Fprintf(&b, "\n\nThe API returned HTTP %d", e.Status)
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s: %s)", e.Type, e.Code)
	} else if e.Type != "" {
		fmt.Fprintf(&b, " (%s)", e.Type)
	}
	b.WriteString(".")
	if e.RequestID != "" {
		fmt.Fprintf(&b, " Request id: %s.", e.RequestID)
	}
	return b.String()
}

// ignoreNotFound treats a 404 on delete as success: the object is already gone.
func ignoreNotFound(err error) error {
	if transcdr.IsNotFound(err) {
		return nil
	}
	return err
}

// topLevelParam maps an API param such as `config.bucket` or `source.prefix` onto the attribute
// path with the same names, when its first segment is one of attrs. Deeper segments that are
// numbers (array indexes) or unknown stop the path at the last known name.
func topLevelParam(attrs map[string][]string) func(string) (path.Path, bool) {
	return func(param string) (path.Path, bool) {
		parts := strings.Split(param, ".")
		nested, ok := attrs[parts[0]]
		if !ok {
			return path.Path{}, false
		}
		p := path.Root(parts[0])
		if len(parts) > 1 {
			for _, n := range nested {
				if n == parts[1] {
					p = p.AtName(parts[1])
					break
				}
			}
		}
		return p, true
	}
}

// ptr returns the value of a known, non-null string, else nil.
func ptr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func boolPtr(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func int64Ptr(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	n := v.ValueInt64()
	return &n
}

func strOrNull(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func str(s string) *string { return &s }

// known reports whether a value is neither null nor unknown.
func known(v attr.Value) bool {
	return !v.IsNull() && !v.IsUnknown()
}

// keepIfEquivalent returns prior when the API's value means the same thing (per equal), so the
// configuration's spelling stays in state; else the API's value, which Terraform shows as drift.
func keepIfEquivalent(prior types.String, api *string, equal func(a, b string) bool) types.String {
	if api == nil {
		return types.StringNull()
	}
	if known(prior) && equal(prior.ValueString(), *api) {
		return prior
	}
	return types.StringValue(*api)
}

func sameString(a, b string) bool { return a == b }

// sameURL compares URLs the way the API normalises them: scheme and host case-insensitively, and a
// bare host with or without its trailing slash.
func sameURL(a, b string) bool {
	if a == b {
		return true
	}
	ua, errA := url.Parse(strings.TrimSpace(a))
	ub, errB := url.Parse(strings.TrimSpace(b))
	if errA != nil || errB != nil {
		return false
	}
	norm := func(u *url.URL) string {
		p := u.EscapedPath()
		if p == "/" {
			p = ""
		}
		q := ""
		if u.RawQuery != "" {
			q = "?" + u.RawQuery
		}
		return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + p + q
	}
	return norm(ua) == norm(ub)
}

// samePath compares storage paths the way the API normalises them: `incoming/`, `/incoming` and
// `incoming` are one prefix.
func samePath(a, b string) bool {
	return normalizePath(a) == normalizePath(b)
}

func normalizePath(p string) string {
	var parts []string
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part != "." {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/")
}

// stringSet converts a list from the API into a set value.
func stringSet(values []string) types.Set {
	elems := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, types.StringValue(v))
	}
	return types.SetValueMust(types.StringType, elems)
}

func stringList(values []string) types.List {
	elems := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, types.StringValue(v))
	}
	return types.ListValueMust(types.StringType, elems)
}

// setStrings reads a known set of strings.
func setStrings(ctx context.Context, v types.Set, diags *diag.Diagnostics) []string {
	if !known(v) {
		return nil
	}
	var out []string
	diags.Append(v.ElementsAs(ctx, &out, false)...)
	return out
}

// metadataValue is the API's metadata as a map; an empty map stays null when the prior value was
// null, since the API returns `{}` for "none".
func metadataValue(prior types.Map, api map[string]string) types.Map {
	if len(api) == 0 && prior.IsNull() {
		return types.MapNull(types.StringType)
	}
	elems := make(map[string]attr.Value, len(api))
	for k, v := range api {
		elems[k] = types.StringValue(v)
	}
	return types.MapValueMust(types.StringType, elems)
}

func mapStrings(ctx context.Context, v types.Map, diags *diag.Diagnostics) map[string]string {
	out := map[string]string{}
	if !known(v) {
		return out
	}
	diags.Append(v.ElementsAs(ctx, &out, false)...)
	return out
}

// isEmptyJSONObject reports whether raw is `{}` (or null, or missing).
func isEmptyJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	if v == nil {
		return true
	}
	m, ok := v.(map[string]any)
	return ok && len(m) == 0
}

// apiDetail is an API error's full explanation, or the error text.
func apiDetail(err error) string {
	if apiErr, ok := transcdr.AsError(err); ok {
		return errorDetail(apiErr)
	}
	return err.Error()
}

// jsonEqual reports whether two JSON documents hold the same value, whatever their formatting and
// key order. Numbers compare by value (4 and 4.0 are equal).
func jsonEqual(a, b string) bool {
	var va, vb any
	if json.Unmarshal([]byte(a), &va) != nil || json.Unmarshal([]byte(b), &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// joinTicks formats values as a Markdown list of code spans: "`a`, `b`".
func joinTicks(values []string) string {
	return strings.Join(values, "`, `")
}

// nullableString is a known string as a Nullable value, else left out.
func nullableString(v types.String) transcdr.Nullable[string] {
	if known(v) {
		return transcdr.Value(v.ValueString())
	}
	return transcdr.Nullable[string]{}
}

// formatTime renders an API timestamp as RFC 3339.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// timeOrNull renders an optional API timestamp.
func timeOrNull(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(formatTime(*t))
}
