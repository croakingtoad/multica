import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import proxy from "./proxy";

describe("proxy", () => {
  it("rewrites paths without a locale prefix to the default language", () => {
    const req = new NextRequest("http://localhost:4000/docs/agents", {
      nextConfig: { basePath: "/docs" },
    });
    const res = proxy(req);
    expect(res.headers.get("x-middleware-rewrite")).toBe(
      "http://localhost:4000/docs/en/agents",
    );
  });

  it("rewrites the bare docs root to the default-language home", () => {
    const req = new NextRequest("http://localhost:4000/docs/", {
      nextConfig: { basePath: "/docs" },
    });
    const res = proxy(req);
    expect(res.headers.get("x-middleware-rewrite")).toBe(
      "http://localhost:4000/docs/en",
    );
  });

  it("redirects /en prefix stripping it and preserving the query string", () => {
    const req = new NextRequest(
      "http://localhost:4000/docs/en/agents?utm_source=qc",
      { nextConfig: { basePath: "/docs" } },
    );
    const res = proxy(req);
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "http://localhost:4000/docs/agents?utm_source=qc",
    );
  });

  it("passes through non-default language prefixes unchanged", () => {
    const req = new NextRequest("http://localhost:4000/docs/zh/agents", {
      nextConfig: { basePath: "/docs" },
    });
    const res = proxy(req);
    expect(res.status).toBe(200);
    expect(res.headers.get("x-middleware-rewrite")).toBeNull();
    expect(res.headers.get("location")).toBeNull();
  });
});

describe("config", () => {
  it("exports a matcher array", async () => {
    const { config } = await import("./proxy");
    expect(Array.isArray(config.matcher)).toBe(true);
    expect(config.matcher.length).toBeGreaterThan(0);
  });
});
