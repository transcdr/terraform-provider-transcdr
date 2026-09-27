package provider

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

// Write-only secrets are never returned, but the API gives each one that is set a fingerprint: an
// HMAC keyed on the server, so it cannot be computed here, that changes whenever the secret does.
// The fingerprints seen after the last apply or refresh are kept in the resource's private state;
// a refresh that finds a different one knows the secret was changed outside Terraform.

const fingerprintsKey = "secret_fingerprints"

type privateGetter interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}

type privateSetter interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// fingerprintsOf maps each secret that is set to its fingerprint.
func fingerprintsOf(secrets map[string]transcdr.SecretStatus) map[string]string {
	out := make(map[string]string, len(secrets))
	for name, s := range secrets {
		if s.Set {
			out[name] = s.Fingerprint
		}
	}
	return out
}

// saveFingerprints records the fingerprints the API returned.
func saveFingerprints(ctx context.Context, p privateSetter, secrets map[string]transcdr.SecretStatus) diag.Diagnostics {
	b, err := json.Marshal(fingerprintsOf(secrets))
	if err != nil {
		var d diag.Diagnostics
		d.AddError("Could not record the secret fingerprints", err.Error())
		return d
	}
	return p.SetKey(ctx, fingerprintsKey, b)
}

// savedFingerprints are the fingerprints recorded last; ok is false when there are none (a
// resource imported, or created by an older provider).
func savedFingerprints(ctx context.Context, p privateGetter) (saved map[string]string, ok bool, diags diag.Diagnostics) {
	b, diags := p.GetKey(ctx, fingerprintsKey)
	if diags.HasError() || len(b) == 0 {
		return nil, false, diags
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		return nil, false, diags
	}
	return saved, true, diags
}

// changedSecrets names the secrets recorded before whose fingerprint is now different or gone.
func changedSecrets(saved map[string]string, now map[string]transcdr.SecretStatus) map[string]bool {
	changed := map[string]bool{}
	for name, fp := range saved {
		if s, ok := now[name]; !ok || !s.Set || s.Fingerprint != fp {
			changed[name] = true
		}
	}
	return changed
}

// secretDriftWarning tells the user which configured secrets will be sent again.
func secretDriftWarning(diags *diag.Diagnostics, what string, names []string) {
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	diags.AddWarning("Secret changed outside Terraform",
		"The "+what+"'s "+strings.Join(names, ", ")+" no longer matches what Terraform last set (its fingerprint changed). "+
			"The next apply sets the configured value again.")
}
