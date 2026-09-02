export const COMPOSIO_MCP_APPS_FLAG = "composio_mcp_apps";
export const PLUGINS_V1_FLAG = "plugins_v1";
export const BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG =
  "billing_workspace_subscriptions";
// Mirrors server/internal/featureflags/keys.go's ProjectPlans. The backend
// publishes it through frontendPublicFlags and now defaults it ON, so an
// unconfigured deployment serves plans; FF_PROJECT_PLANS=false is retained as
// the operator kill switch.
//
// Call sites still pass `false` to useFeatureEnabled on purpose: the key is
// always present in the /api/config payload, so that fallback only applies
// before config has loaded, where failing closed is correct — a `true`
// fallback would flash plan affordances on a deployment that has the kill
// switch on.
export const PROJECT_PLANS_FLAG = "project_plans";
