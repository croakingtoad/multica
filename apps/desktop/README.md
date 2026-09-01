# Multica Desktop

## Packaged build channels

Desktop has two package-time channels. The tracked builder configuration is
never edited between them:

```bash
# Stable (MULTICA_CHANNEL defaults to stable)
pnpm -C apps/desktop package:stable -- --linux dir --x64 --publish never

# Dev
pnpm -C apps/desktop package:dev -- --linux dir --x64 --publish never
```

For a normal installer, omit `dir`; electron-builder then creates the host's
configured installer formats. On macOS, an unsigned local build also needs
`CSC_IDENTITY_AUTO_DISCOVERY=false`. In PowerShell, invoke `package` after
setting `$env:MULTICA_CHANNEL = "dev"` and
`$env:MULTICA_SHARED_STATE_COMPAT = "stable"` instead of using the POSIX-only
`package:dev` convenience script.

Every package must declare whether its client-side writes remain compatible
with stable:

```bash
# Ordinary dev package: shared writes are compatible
MULTICA_CHANNEL=dev MULTICA_SHARED_STATE_COMPAT=stable node apps/desktop/scripts/package.mjs --linux dir --x64

# Breaking package: read-only until a disposable target is confirmed
MULTICA_CHANNEL=dev MULTICA_SHARED_STATE_COMPAT=breaking node apps/desktop/scripts/package.mjs --linux dir --x64
```

Packaging fails if `MULTICA_SHARED_STATE_COMPAT` is absent or has another
value. The declaration is compiled into the app and written to builder
metadata; it is never read from the launch environment. A breaking dev build
allows GET and HEAD, but its central API client blocks POST, PATCH, PUT, and
DELETE until the user confirms the named backend/workspace as sacrificial.
This guard enforces the packager's declaration; it does not detect schema drift
automatically.

Stable keeps the existing `Multica` / `ai.multica.desktop` /
`multica-desktop` / `multica://` identities. Dev bakes in `Multica Dev` /
`ai.multica.desktop.dev` / `multica-desktop-dev` / `multica-dev://`, stores
Electron state under a separate `Multica Dev` userData directory, disables
updates, and uses its own daemon profile and `desktop_prefs-dev.json`.
`~/.multica/desktop.json` remains shared deliberately so both channels select
the same backend.

`MULTICA_CHANNEL` is consumed only while bundling and packaging. The selected
metadata is compiled into the Electron bundles; setting `MULTICA_CHANNEL=dev`
when launching an already-built stable binary cannot change its channel.

### Side-by-side verification

Use a disposable home so the check cannot touch a real installation:

1. Build stable, then dev with the commands above. Stable is written to
   `apps/desktop/dist/linux-unpacked`; dev is written to
   `apps/desktop/dist/dev/linux-unpacked` and does not clean the stable output.
2. Under one temporary `HOME` and `XDG_CONFIG_HOME`, create sentinel stable
   files at `Multica/`, `.multica/desktop_prefs.json`, and
   `.multica/profiles/desktop-<host>/`, plus a shared `.multica/desktop.json`.
3. Hash those sentinels, launch only the dev executable under Xvfb, then hash
   them again. Matching hashes prove that building and running dev left stable
   state and the shared backend selector untouched.
4. Launch both unpacked executables together. Two live PIDs plus simultaneous
   `Multica/SingletonLock` and `Multica Dev/SingletonLock` entries prove the
   builds do not share a single-instance lock.

macOS packaging and the two-app dock demonstration must be run on a Mac. Use
the same commands with `--mac --arm64` (or `--x64`) and confirm that both apps
remain open, have distinct bundle identifiers, and that `multica://` still
opens stable while `multica-dev://` opens dev.
