// @vitest-environment node
import { mkdir, mkdtemp, writeFile } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";
import { describe, expect, it } from "vitest";
import {
  desktopConfigPath,
  loadRuntimeConfig,
} from "./runtime-config-loader";

describe("desktopConfigPath", () => {
  it("keeps the stable config path unchanged", () => {
    expect(desktopConfigPath("C:\\Users\\Marty", "desktop.json")).toBe(
      join("C:\\Users\\Marty", ".multica", "desktop.json"),
    );
  });

  it("uses the channel-conventional -dev suffix for dev", () => {
    expect(desktopConfigPath("C:\\Users\\Marty", "desktop-dev.json")).toBe(
      join("C:\\Users\\Marty", ".multica", "desktop-dev.json"),
    );
  });
});

describe("loadRuntimeConfig", () => {
  it("uses dev env and ignores desktop.json during electron-vite dev", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    const configPath = join(dir, "desktop.json");
    await writeFile(
      configPath,
      JSON.stringify({ schemaVersion: 1, apiUrl: "https://prod.example.com" }),
    );

    await expect(
      loadRuntimeConfig({
        isDev: true,
        configPath,
        env: {
          apiUrl: "http://localhost:8080",
          wsUrl: "ws://localhost:8080/ws",
          appUrl: "http://localhost:3000",
        },
      }),
    ).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: "http://localhost:8080",
        wsUrl: "ws://localhost:8080/ws",
        appUrl: "http://localhost:3000",
      },
    });
  });

  it("uses cloud defaults when packaged config is absent", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    await expect(
      loadRuntimeConfig({
        isDev: false,
        configPath: join(dir, "missing.json"),
        env: {},
      }),
    ).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: "https://api.multica.ai",
        wsUrl: "wss://api.multica.ai/ws",
        appUrl: "https://multica.ai",
      },
    });
  });

  it("parses a valid packaged desktop.json", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    const configPath = join(dir, "desktop.json");
    await writeFile(
      configPath,
      JSON.stringify({ schemaVersion: 1, apiUrl: "https://api.example.com" }),
    );

    await expect(
      loadRuntimeConfig({ isDev: false, configPath, env: {} }),
    ).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: "https://api.example.com",
        wsUrl: "wss://api.example.com/ws",
        appUrl: "https://example.com",
      },
    });
  });

  it("reads each channel's config when both files are present", async () => {
    const homePath = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    const configDir = join(homePath, ".multica");
    const stablePath = desktopConfigPath(homePath, "desktop.json");
    const devPath = desktopConfigPath(homePath, "desktop-dev.json");
    await mkdir(configDir);
    await Promise.all([
      writeFile(
        stablePath,
        JSON.stringify({
          schemaVersion: 1,
          apiUrl: "https://stable.example.com",
        }),
      ),
      writeFile(
        devPath,
        JSON.stringify({ schemaVersion: 1, apiUrl: "https://dev.example.com" }),
      ),
    ]);

    const [stable, dev] = await Promise.all([
      loadRuntimeConfig({ isDev: false, configPath: stablePath, env: {} }),
      loadRuntimeConfig({ isDev: false, configPath: devPath, env: {} }),
    ]);

    expect(stable).toMatchObject({
      ok: true,
      config: { apiUrl: "https://stable.example.com" },
    });
    expect(dev).toMatchObject({
      ok: true,
      config: { apiUrl: "https://dev.example.com" },
    });
  });

  it("fails closed when packaged desktop.json is invalid", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    const configPath = join(dir, "desktop.json");
    await writeFile(configPath, "{");

    const result = await loadRuntimeConfig({ isDev: false, configPath, env: {} });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.message).toContain(configPath);
      expect(result.error.message).toContain("Invalid desktop runtime config JSON");
    }
  });
});
