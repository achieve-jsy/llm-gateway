package protocol

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// ConversionError is returned by ApplyLossPolicy when the active loss policy
// rejects a conversion. It carries the rejected diagnostics and an HTTP status
// code suitable for the client response.
type ConversionError struct {
	// Diagnostics is the subset of conversion diagnostics that triggered the
	// rejection.
	Diagnostics []types.ConversionDiagnostic

	// HTTPStatus is the HTTP status code to return to the client.
	HTTPStatus int
}

// Error implements the error interface, returning a human-readable summary of
// the rejected diagnostics.
func (e *ConversionError) Error() string {
	if e == nil || len(e.Diagnostics) == 0 {
		return "conversion rejected by loss policy"
	}
	msgs := make([]string, 0, len(e.Diagnostics))
	for _, d := range e.Diagnostics {
		if d.Message != "" {
			msgs = append(msgs, d.Message)
		} else {
			msgs = append(msgs, d.Code)
		}
	}
	return fmt.Sprintf("conversion rejected by loss policy: %s", strings.Join(msgs, "; "))
}

// CollectDiagnostics merges relaykit diagnostics with bridge-level diagnostics
// into a single deduplicated slice. The From and To fields are set from the
// session's source/target formats. Duplicates are detected by Code only; when
// the same Code appears in both lists, the first occurrence is kept.
func CollectDiagnostics(session *Session, relayDiags []types.ConversionDiagnostic, bridgeDiags []types.ConversionDiagnostic) []types.ConversionDiagnostic {
	total := len(relayDiags) + len(bridgeDiags)
	if total == 0 {
		return nil
	}

	seen := make(map[string]struct{}, total)
	result := make([]types.ConversionDiagnostic, 0, total)

	appendUnique := func(diags []types.ConversionDiagnostic) {
		for _, d := range diags {
			if d.Code == "" {
				continue
			}
			if _, ok := seen[d.Code]; ok {
				continue
			}
			seen[d.Code] = struct{}{}
			// Set From/To from session if available
			if session != nil {
				d.From = session.SourceFormat
				d.To = session.TargetFormat
			}
			result = append(result, d)
		}
	}

	appendUnique(relayDiags)
	appendUnique(bridgeDiags)

	if len(result) == 0 {
		return nil
	}
	return result
}

// FormatLossHeader formats a slice of diagnostics as the value for the
// X-Protocol-Loss HTTP response header. It returns a comma-separated list of
// diagnostic codes, truncated to 512 ASCII bytes with a "..." suffix when the
// limit is exceeded.
func FormatLossHeader(diags []types.ConversionDiagnostic) string {
	if len(diags) == 0 {
		return ""
	}

	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		if d.Code != "" {
			codes = append(codes, d.Code)
		}
	}
	if len(codes) == 0 {
		return ""
	}

	header := strings.Join(codes, ",")
	if len(header) <= 512 {
		return header
	}

	// Truncate to 512 ASCII bytes. We need at most 509 bytes of content so the
	// "..." suffix (3 bytes) stays within the 512-byte ceiling.
	const maxTotal = 512
	const ellipsis = "..."
	if len(header) <= maxTotal {
		return header
	}
	// Max prefix that still fits with ellipsis
	maxPrefix := maxTotal - len(ellipsis)
	// Try to cut at the last comma within the prefix to avoid splitting a code
	lastComma := strings.LastIndex(header[:maxPrefix], ",")
	if lastComma > 0 {
		return header[:lastComma] + ellipsis
	}
	// No comma found — a single code is too long; trim raw
	if maxPrefix > len(header) {
		maxPrefix = len(header)
	}
	return header[:maxPrefix] + ellipsis
}

// ApplyLossPolicy evaluates the active loss policy against a slice of
// diagnostics and returns a *ConversionError if the policy mandates rejection.
//
// Policy semantics:
//   - "allow" (default): never rejects.
//   - "safe": rejects only diagnostics with Severity == "error".
//   - "strict": rejects all diagnostics regardless of severity.
//
// Empty or unknown policies default to "allow".
func ApplyLossPolicy(policy types.ConversionLossPolicy, diags []types.ConversionDiagnostic) error {
	if len(diags) == 0 {
		return nil
	}

	switch policy {
	case types.ConversionLossPolicyAllow, "":
		return nil
	case types.ConversionLossPolicySafe:
		rejected := make([]types.ConversionDiagnostic, 0)
		for _, d := range diags {
			if d.Severity == types.ConversionDiagnosticError {
				rejected = append(rejected, d)
			}
		}
		if len(rejected) == 0 {
			return nil
		}
		return &ConversionError{
			Diagnostics: rejected,
			HTTPStatus:  400,
		}
	case types.ConversionLossPolicyStrict:
		return &ConversionError{
			Diagnostics: diags,
			HTTPStatus:  400,
		}
	default:
		// Unknown policy → allow
		return nil
	}
}

// JoinDiagnostics appends relaykit and bridge diagnostics to the session and
// returns the session for fluent chaining.
//
// relayResult may be nil when no relaykit conversion was performed (e.g., the
// request was rejected before reaching the converter).
// bridgeDiags are diagnostics produced by protocol-level bridge code outside
// of relaykit.
func JoinDiagnostics(session *Session, relayResult *relayconvert.RequestResult, bridgeDiags []types.ConversionDiagnostic) *Session {
	if session == nil {
		return nil
	}
	diags := CollectDiagnostics(session, relayDiagnostics(relayResult), bridgeDiags)
	session.Diagnostics = append(session.Diagnostics, diags...)
	return session
}

// relayDiagnostics extracts diagnostics from a relaykit RequestResult, or nil
// when the result is nil.
func relayDiagnostics(result *relayconvert.RequestResult) []types.ConversionDiagnostic {
	if result == nil {
		return nil
	}
	return result.Diagnostics
}
