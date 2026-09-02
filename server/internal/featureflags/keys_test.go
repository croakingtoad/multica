package featureflags

import (
	"context"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestResourceLabelsCompatDecisionStaysEnabled(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if !flags[resourceLabelsCompat] {
		t.Fatal("resource labels must stay enabled for installed clients")
	}
}

// MUL-5345: hang stack capture is gone from this build, but v0.4.13–v0.4.18 are
// installed and still hold a debugger channel open on every renderer whenever
// this key arrives as `true`. Those clients are fail-closed on absence, so NOT
// publishing the key is what disarms them — re-adding it would put a flag flip
// back within reach of a fleet that can no longer produce a usable stack.
func TestDesktopHangStackCaptureIsNotPublished(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if _, published := flags["desktop_hang_stack_capture"]; published {
		t.Fatal("hang stack capture must stay unpublished so installed clients keep their debugger channels closed")
	}
}

func TestAgentBuilderCompatDecisionStaysEnabled(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if !flags[agentBuilderCompat] {
		t.Fatal("agent builder must stay enabled for installed clients")
	}
}

func TestAgentSkillTogglesCompatDecisionStaysEnabled(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if !flags[agentSkillTogglesCompat] {
		t.Fatal("agent skill toggles must stay enabled for installed v0.4.0 clients")
	}
}

// MUL-6643: the server-side rollout gate on creating a custom status is gone,
// but the key stays unpublished on purpose. v0.4.30 shipped the feature without
// the four fixes that landed in v0.4.31 — custom-status cards render in the
// wrong board column (MUL-6409), the timeline glyph loses the status identity
// (MUL-6413), built-in colors are wrong (MUL-6440), and the catalog does not
// sync over the realtime channel (MUL-6458). Those clients gate their "New
// status" button on this key and fail closed on absence, so NOT publishing it
// is what keeps a client that cannot render the result from producing one.
//
// Clients from v0.4.33 read no flag at all, so they get the button the moment
// they update — the key never has to be published again.
func TestCustomIssueStatusesIsNotPublished(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if _, published := flags["custom_issue_statuses"]; published {
		t.Fatal("custom_issue_statuses must stay unpublished so pre-v0.4.33 clients keep creation hidden")
	}
}

func TestPluginsV1DefaultsOff(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if flags[PluginsV1] {
		t.Fatal("plugins_v1 must stay disabled unless explicitly enabled")
	}
}

// LOCO-878: plans are default-on with the flag retained. The route gate and
// the frontend publication resolve their default at two separate call sites,
// so this walks all three environment states and holds the two together — a
// route must never serve a surface /api/config reports as off, or vice versa.
// FF_PROJECT_PLANS=false surviving as a kill switch is the point of flipping
// the default rather than deleting the gate.
func TestProjectPlansEnvironmentStatesKeepBackendAndFrontendAligned(t *testing.T) {
	t.Setenv(featureflag.EnvFlagFile, "")
	falseValue := "false"
	trueValue := "true"
	tests := []struct {
		name     string
		envValue *string
		want     bool
	}{
		{name: "unset defaults on", want: true},
		{name: "false is kill switch", envValue: &falseValue, want: false},
		{name: "true stays on", envValue: &trueValue, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setOptionalEnv(t, "FF_PROJECT_PLANS", test.envValue)
			flags, err := featureflag.NewServiceFromEnv()
			if err != nil {
				t.Fatalf("build feature flag service: %v", err)
			}

			backendEnabled := ProjectPlansEnabled(context.Background(), flags)
			if backendEnabled != test.want {
				t.Fatalf("backend project_plans = %t, want %t", backendEnabled, test.want)
			}
			frontendEnabled, published := EvaluateFrontendPublicFlags(context.Background(), flags)[ProjectPlans]
			if !published {
				t.Fatal("project_plans must be published for the frontend")
			}
			if frontendEnabled != backendEnabled {
				t.Fatalf("frontend project_plans = %t, backend = %t", frontendEnabled, backendEnabled)
			}
		})
	}
}

// project_plans is the only default that moved. Every other published flag
// stays off when no provider supplies a decision, and a key absent from
// flagDefaults keeps the old fail-closed behaviour.
func TestOnlyProjectPlansDefaultsOn(t *testing.T) {
	published := EvaluateFrontendPublicFlags(context.Background(), nil)
	for _, key := range frontendPublicFlags {
		want := key == ProjectPlans
		if published[key] != want {
			t.Fatalf("published default for %q = %t, want %t", key, published[key], want)
		}
		if got := defaultFor(key); got != want {
			t.Fatalf("defaultFor(%q) = %t, want %t", key, got, want)
		}
	}
	if BillingWorkspaceSubscriptionsEnabled(context.Background(), nil) {
		t.Fatal("billing_workspace_subscriptions must stay disabled by default")
	}
	if ComposioMCPAppsEnabled(context.Background(), nil) {
		t.Fatal("composio_mcp_apps must stay disabled by default")
	}
	if len(flagDefaults) != 1 {
		t.Fatalf("flagDefaults must list project_plans only, got %v", flagDefaults)
	}
	if defaultFor("some_flag_nobody_registered") {
		t.Fatal("a key absent from flagDefaults must default to off")
	}
}

func setOptionalEnv(t *testing.T, key string, value *string) {
	t.Helper()
	previous, present := os.LookupEnv(key)
	t.Cleanup(func() {
		var err error
		if present {
			err = os.Setenv(key, previous)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			t.Errorf("restore %s: %v", key, err)
		}
	})

	var err error
	if value == nil {
		err = os.Unsetenv(key)
	} else {
		err = os.Setenv(key, *value)
	}
	if err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

func TestPluginSubFlagsAreNotPublished(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	for _, retired := range []string{"private_plugins_v1", "remote_mcp_plugins_v1"} {
		if _, published := flags[retired]; published {
			t.Fatalf("retired Plugin sub-flag %q must not be published", retired)
		}
	}
}
