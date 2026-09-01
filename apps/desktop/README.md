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
`CSC_IDENTITY_AUTO_DISCOVERY=false`.

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

### Windows installers

Run these commands from the repository root in PowerShell. Use the `pnpm`
script entry point shown here rather than invoking `package.mjs` directly;
electron-builder needs pnpm's package-manager environment to collect workspace
dependencies correctly.

```powershell
corepack enable
pnpm install --frozen-lockfile

$env:MULTICA_CHANNEL = "stable"
$env:MULTICA_SHARED_STATE_COMPAT = "stable"
pnpm -C apps/desktop package -- --win nsis --x64 --publish never

$env:MULTICA_CHANNEL = "dev"
$env:MULTICA_SHARED_STATE_COMPAT = "stable"
pnpm -C apps/desktop package -- --win nsis --x64 --publish never
```

The stable installer lands at
`apps\desktop\dist\multica-desktop-<version>-windows-x64.exe`. The dev
installer lands at
`apps\desktop\dist\dev\multica-desktop-dev-<version>-windows-x64.exe`.
Building dev cleans only `dist\dev`, so it leaves the stable artifact in place.
Use `--arm64` instead of `--x64` for Windows on Arm.

The generated NSIS configuration intentionally pins one-click, per-user
installs and both shortcuts. With the repository's locked electron-builder
26.8.1, the install directory and Windows executable name derive from
`productName`, and each uninstall GUID is UUIDv5 of `appId`:

| Surface | Stable | Dev |
| --- | --- | --- |
| Install directory | `%LOCALAPPDATA%\Programs\Multica` | `%LOCALAPPDATA%\Programs\Multica Dev` |
| Executable | `Multica.exe` | `Multica Dev.exe` |
| Start Menu / desktop shortcut | `Multica.lnk` | `Multica Dev.lnk` |
| Add/Remove Programs | `Multica <version>` | `Multica Dev <version>` |
| Uninstall GUID | `d8b75c36-d208-59aa-9acd-26838c159dc3` | `4745ca0c-31cb-5f18-beb0-bf3d33106e6c` |
| Electron userData | `%APPDATA%\Multica` | `%APPDATA%\Multica Dev` |
| App User Model ID | `ai.multica.desktop` | `ai.multica.desktop.dev` |
| Deep-link scheme | `multica://` | `multica-dev://` |

#### Windows side-by-side verification

The cross-build proves that both Windows packages can be produced and that
their embedded identities differ. It cannot prove that two installed apps
coexist on Windows; run this checklist on the target Windows machine and keep a
screenshot of step 4 as the final acceptance evidence.

1. **Record the stable baseline before installing dev.** If the daily stable
   app is already installed, do not reinstall it. Otherwise, run the stable
   installer first. Close both apps, then capture the stable executable hash,
   shared backend-selector hash, and protocol command:

   ```powershell
   $stableExe = "$env:LOCALAPPDATA\Programs\Multica\Multica.exe"
   $desktopConfig = "$HOME\.multica\desktop.json"
   $stableProtocol = "Registry::HKEY_CURRENT_USER\Software\Classes\multica\shell\open\command"
   $before = [ordered]@{
     StableExeHash = (Get-FileHash $stableExe -Algorithm SHA256).Hash
     DesktopConfigHash = (Get-FileHash $desktopConfig -Algorithm SHA256).Hash
     StableProtocolCommand = (Get-ItemProperty $stableProtocol).'(default)'
   }
   $before | ConvertTo-Json | Set-Content "$env:TEMP\multica-stable-before.json"
   $before
   ```

   Pass means all three values are present. Keep that PowerShell session open;
   later steps compare against `$before`.

2. **Install dev and verify its installer identity.** Run the dev installer,
   then inspect both installation records:

   ```powershell
   $stableGuid = 'd8b75c36-d208-59aa-9acd-26838c159dc3'
   $devGuid = '4745ca0c-31cb-5f18-beb0-bf3d33106e6c'
   Get-ItemProperty "HKCU:\Software\$stableGuid" |
     Select-Object InstallLocation, ShortcutName
   Get-ItemProperty "HKCU:\Software\$devGuid" |
     Select-Object InstallLocation, ShortcutName
   Get-ItemProperty "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\$stableGuid" |
     Select-Object DisplayName, DisplayVersion
   Get-ItemProperty "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\$devGuid" |
     Select-Object DisplayName, DisplayVersion
   $desktopFolder = [Environment]::GetFolderPath('Desktop')
   Test-Path "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Multica.lnk"
   Test-Path "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Multica Dev.lnk"
   Test-Path "$desktopFolder\Multica.lnk"
   Test-Path "$desktopFolder\Multica Dev.lnk"
   ```

   Pass values are the two distinct directories, shortcuts, and Add/Remove
   Programs names in the table above, followed by four `True` shortcut checks.

3. **Verify executable and userData names.** Launch each app once, then run:

   ```powershell
   Test-Path "$env:LOCALAPPDATA\Programs\Multica\Multica.exe"
   Test-Path "$env:LOCALAPPDATA\Programs\Multica Dev\Multica Dev.exe"
   Test-Path "$env:APPDATA\Multica"
   Test-Path "$env:APPDATA\Multica Dev"
   ```

   Pass is four `True` results. `executableName` and `StartupWMClass` are
   Linux-only settings; the two `.exe` names above demonstrate that Windows
   used `productName` and did not fall back to the stable name.

4. **Prove both single-instance domains coexist.** With both windows open, run:

   ```powershell
   Get-CimInstance Win32_Process |
     Where-Object Name -in @('Multica.exe', 'Multica Dev.exe') |
     Select-Object ProcessId, Name, ExecutablePath
   ```

   Pass means live rows exist for both executable paths at the same time.
   Capture a screenshot showing both taskbar entries and windows: dev must have
   the dev icon and a title prefixed with `[DEV]`; stable must retain its normal
   icon and title. Coexisting processes are the Windows evidence that the two
   `requestSingleInstanceLock()` calls did not collide.

5. **Verify deep links and the stable registration.** Read both per-user
   protocol commands and open one URL through each scheme:

   ```powershell
   $stableCommand = (Get-ItemProperty 'Registry::HKEY_CURRENT_USER\Software\Classes\multica\shell\open\command').'(default)'
   $devCommand = (Get-ItemProperty 'Registry::HKEY_CURRENT_USER\Software\Classes\multica-dev\shell\open\command').'(default)'
   $stableCommand
   $devCommand
   Start-Process 'multica://issues'
   Start-Process 'multica-dev://issues'
   ```

   Pass means `$stableCommand -eq $before.StableProtocolCommand`, the stable
   command names `Multica.exe`, the dev command names `Multica Dev.exe`, and
   each URL focuses the matching window. The unchanged stable command is the
   registry proof that dev registration did not replace `multica://`.

6. **Verify daemon isolation and the dev auto-start default.** In dev, open
   Settings > Daemon before manually starting it: Auto-start must be off. If
   `%USERPROFILE%\.multica\desktop_prefs-dev.json` exists, its `autoStart`
   value must be `false`; a missing file also means the compiled false default
   is in effect. Stable uses `desktop_prefs.json`.

   Derive the profile names and health ports from the shared target URL:

   ```powershell
   $desktop = Get-Content "$HOME\.multica\desktop.json" | ConvertFrom-Json
   $authority = ([uri]$desktop.apiUrl).Authority.Replace(':', '-').ToLowerInvariant()
   $stableProfile = "desktop-$authority"
   $devProfile = "$stableProfile-dev"
   function Get-MulticaHealthPort([string]$profile) {
     $sum = 0
     [Text.Encoding]::UTF8.GetBytes($profile) | ForEach-Object { $sum += $_ }
     19515 + ($sum % 1000)
   }
   $stablePort = Get-MulticaHealthPort $stableProfile
   $devPort = Get-MulticaHealthPort $devProfile
   $stableProfile, $stablePort, $devProfile, $devPort
   ```

   Pass means the profile names end in `desktop-<host>` and
   `desktop-<host>-dev`, both directories exist under
   `%USERPROFILE%\.multica\profiles`, and the ports differ from each other and
   from `19514`. After manually starting both daemons, these commands must
   report the matching profile and `running` state:

   ```powershell
   Invoke-RestMethod "http://127.0.0.1:$stablePort/health" |
     Select-Object state, profile
   Invoke-RestMethod "http://127.0.0.1:$devPort/health" |
     Select-Object state, profile
   ```

7. **Verify updates stay disabled in dev.** Open Settings > Updates in the dev
   window. Automatic updates must be off. Click Check now; pass is the exact
   message `Updates are disabled for the dev channel.` Stable's update controls
   remain enabled.

8. **Prove dev left stable state untouched.** Close dev, then compare the
   stable executable, shared selector, and protocol to the baseline:

   ```powershell
   $after = [ordered]@{
     StableExeHash = (Get-FileHash $stableExe -Algorithm SHA256).Hash
     DesktopConfigHash = (Get-FileHash $desktopConfig -Algorithm SHA256).Hash
     StableProtocolCommand = (Get-ItemProperty $stableProtocol).'(default)'
   }
   $after
   $after.StableExeHash -eq $before.StableExeHash
   $after.DesktopConfigHash -eq $before.DesktopConfigHash
   $after.StableProtocolCommand -eq $before.StableProtocolCommand
   ```

   Pass is three `True` results. Also make one harmless server-side change in
   either GUI (for example, rename a disposable issue) and confirm it appears
   in the other; that proves both isolated clients still use the shared
   `desktop.json` backend selector.
