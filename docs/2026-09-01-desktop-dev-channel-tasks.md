# Desktop Dev Channel Task Breakdown

**Goal:** Add a package-time `MULTICA_CHANNEL=dev` Desktop flavor that can run beside stable without sharing application identity, local UI state, deep links, updates, daemon control state, or a single-instance lock.

**Approach:** Keep channel metadata in one package-time module, inject the selected metadata as compile-time literals through electron-vite, and pass the same metadata to electron-builder without editing tracked configuration between builds. Preserve the existing electron-vite Canary path, keep stable defaults unchanged, and cover every isolation boundary with focused tests before performing a Linux side-by-side smoke test.

**Skills:** @js-typescript-dev, @task-breakdown, @multica-runtimes-and-repos

**Tech Details:** TypeScript, Node.js ESM, electron-vite, electron-builder, Vitest, pnpm 10

---

### Task 1: Canonical package-time channel metadata

**Files:**
- Create: `apps/desktop/scripts/build-channel.mjs`
- Create: `apps/desktop/scripts/build-channel.d.mts`
- Create: `apps/desktop/scripts/build-channel.test.mjs`
- Modify: `apps/desktop/electron.vite.config.ts`
- Modify: `apps/desktop/vitest.config.ts`

**Step 1: Write the failing tests**

Test that an unset or `stable` `MULTICA_CHANNEL` resolves to stable, `dev` resolves to a distinct complete identity, invalid values fail, and Vite definitions contain literal metadata rather than a runtime environment lookup.

**Step 2: Run the tests to verify they fail**

Run: `pnpm -C apps/desktop exec vitest run scripts/build-channel.test.mjs`

Expected: FAIL because `scripts/build-channel.mjs` does not exist.

**Step 3: Write the minimal implementation**

Create a frozen `stable`/`dev` metadata table containing product name, app ID, executable, WM class, protocol, icon paths, title prefix/fallback, daemon profile/prefs isolation, auto-start default, and updater policy. Export `resolveBuildChannel(env)` and compile-time `define` values, then use them in electron-vite and Vitest configuration.

**Step 4: Verify the focused tests**

Run: `pnpm -C apps/desktop exec vitest run scripts/build-channel.test.mjs`

Expected: PASS.

### Task 2: electron-builder identity and repeatable package commands

**Files:**
- Modify: `apps/desktop/scripts/package.mjs`
- Modify: `apps/desktop/scripts/package.test.mjs`
- Modify: `apps/desktop/package.json`
- Modify: `apps/desktop/README.md`

**Step 1: Write the failing tests**

Extend `package.test.mjs` to assert stable defaults are unchanged and dev arguments override `appId`, `productName`, packaged `productName`, Linux executable/WM class, protocol, icon paths, and artifact names.

**Step 2: Run the tests to verify they fail**

Run: `pnpm -C apps/desktop exec vitest run scripts/package.test.mjs`

Expected: FAIL because builder arguments are not channel-aware.

**Step 3: Write the minimal implementation**

Resolve the channel once at package-script startup, pass its literal metadata into electron-vite, and append channel-derived electron-builder overrides for each target. Add `package:stable` and `package:dev` scripts and document exact Linux/macOS commands and verification steps.

**Step 4: Verify the focused tests**

Run: `pnpm -C apps/desktop exec vitest run scripts/package.test.mjs scripts/build-channel.test.mjs`

Expected: PASS.

### Task 3: Runtime identity, deep links, title, icons, and updater

**Files:**
- Create: `apps/desktop/src/shared/build-channel.ts`
- Create: `apps/desktop/src/shared/build-channel.test.ts`
- Modify: `apps/desktop/src/main/index.ts`
- Modify: `apps/desktop/src/main/updater.ts`
- Modify: `apps/desktop/src/main/updater.test.ts`
- Modify: `apps/desktop/src/renderer/src/routes.tsx`
- Modify: `apps/desktop/src/renderer/src/hooks/use-document-title.ts`

**Step 1: Write the failing tests**

Test central title decoration, distinct packaged userData/lock identities, stable title behavior, and a dev updater mode that registers IPC but never schedules, checks, downloads, or installs updates.

**Step 2: Run the tests to verify they fail**

Run: `pnpm -C apps/desktop exec vitest run src/shared/build-channel.test.ts src/main/updater.test.ts`

Expected: FAIL because build-channel runtime helpers and updater disabling do not exist.

**Step 3: Write the minimal implementation**

Read only injected compile-time constants in runtime code. Set packaged dev name/userData before `requestSingleInstanceLock`, select the channel protocol/AppUserModelID/runtime icon, decorate route and issue-window titles centrally, and keep updater IPC available in a disabled no-network mode for dev.

**Step 4: Verify the focused tests**

Run: `pnpm -C apps/desktop exec vitest run src/shared/build-channel.test.ts src/main/updater.test.ts`

Expected: PASS.

### Task 4: Daemon profile and preference isolation

**Files:**
- Modify: `apps/desktop/src/main/daemon-profile.ts`
- Modify: `apps/desktop/src/main/daemon-profile.test.ts`
- Create: `apps/desktop/src/main/daemon-prefs.ts`
- Create: `apps/desktop/src/main/daemon-prefs.test.ts`
- Modify: `apps/desktop/src/main/daemon-manager.ts`

**Step 1: Write the failing tests**

Test that dev appends its channel suffix to every derived profile, stable names remain byte-for-byte unchanged, dev/stable ports differ for the same host, dev uses `desktop_prefs-dev.json`, and dev defaults `autoStart` to false.

**Step 2: Run the tests to verify they fail**

Run: `pnpm -C apps/desktop exec vitest run src/main/daemon-profile.test.ts src/main/daemon-prefs.test.ts`

Expected: FAIL because channel-aware profile and preference helpers do not exist.

**Step 3: Write the minimal implementation**

Thread the injected channel metadata through profile derivation and preference path/default construction. Do not change `runtime-config-loader.ts`; `~/.multica/desktop.json` remains shared.

**Step 4: Verify the focused tests**

Run: `pnpm -C apps/desktop exec vitest run src/main/daemon-profile.test.ts src/main/daemon-prefs.test.ts`

Expected: PASS.

### Task 5: Integrated verification and commit

**Files:**
- Review: all files changed above

**Step 1: Run Desktop verification**

Run: `pnpm -C apps/desktop test && pnpm -C apps/desktop typecheck && pnpm -C apps/desktop lint`

Expected: all commands exit 0.

**Step 2: Build both Linux flavors**

Run: `pnpm -C apps/desktop package:stable -- --linux dir --x64 --publish never`

Run: `pnpm -C apps/desktop package:dev -- --linux dir --x64 --publish never`

Expected: distinct unpacked executable names and packaged identities.

**Step 3: Prove isolation in a disposable home**

Launch both unpacked executables under one temporary `HOME`/`XDG_CONFIG_HOME` and Xvfb, verify both PIDs remain alive, and verify separate `Multica` / `Multica Dev` userData lock files. Hash sentinel stable userData, stable daemon prefs/profile, and shared `desktop.json` before and after launching dev; expect no hash changes.

**Step 4: Review and commit**

Run: `git diff --check && git status --short`

Stage only the task files, then commit with: `feat(desktop): add isolated dev build channel`.

Expected: a clean scoped commit on `feature/loco-785-dev-channel`, with no push.
