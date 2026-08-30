export const COMPOSIO_MCP_APPS_FLAG = "composio_mcp_apps";
export const PLUGINS_V1_FLAG = "plugins_v1";
export const BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG =
  "billing_workspace_subscriptions";
// Mirrors server/internal/featureflags/keys.go's ProjectPlans. Deliberately
// not in that file's frontendPublicFlags list until the plan read API and
// panes exist, so the backend never publishes it and this always evaluates
// to the `false` default passed at each call site.
export const PROJECT_PLANS_FLAG = "project_plans";
