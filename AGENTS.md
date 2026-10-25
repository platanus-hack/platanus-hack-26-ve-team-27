## Agent skills

### Issue tracker

Issues are tracked in this repo's GitHub Issues, using the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary: needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## Build, layout and conventions

### Before merging

Run the build and the demo path locally before merging anything. There is no CI on pull requests, so this is the only gate.

```bash
scripts/build.sh
```

It runs, in order: `go vet ./...`, `go test ./...` and `go build ./...` in `server/`, then `./gradlew assembleDebug testDebugUnitTest` at the repo root. It works from any directory and exits non-zero on the first failure. After it is green, install the debug APK on a phone (`adb install -r app/build/outputs/apk/debug/app-debug.apk`), start the server (`cd server && go run ./cmd/server`) and walk the demo path by hand.

Setup it expects: Go 1.24+, a JDK (on macOS the script falls back to Android Studio's bundled JBR when `JAVA_HOME` is unset), and the Android SDK via `local.properties` (`sdk.dir=...`, gitignored) or `ANDROID_HOME`.

### Layout

```
app/                     Android app (Aló Mamá)
  src/main/java/com/team27/mama/
  src/test/java/...      JUnit4 unit tests
mesh/                    BLE mesh library (bitchat-android code, GPL-3.0)
  src/main/java/com/team27/mama/mesh/   MeshGateway (public API) + upstream packages
  NOTICE.md              upstream URL, commit and file provenance
server/                  Go server (its own Go module)
  cmd/server/            main.go + routes.go (newMux registers every route)
  internal/              server packages, one per subdirectory
  migrations/            reserved for SQL migrations
scripts/build.sh         the one local build-and-test command
settings.gradle.kts, build.gradle.kts, gradle/libs.versions.toml, gradlew   Gradle root
LICENSE                  GPL-3.0
```

### Conventions

- **Gradle:** the repo root is the Gradle root. Kotlin DSL, versions only in `gradle/libs.versions.toml`. Two Android modules: `app/` (Aló Mamá) and `mesh/` (Android library with the BLE mesh copied from bitchat-android under GPL-3.0, kept in its own module to isolate third-party code; see `mesh/NOTICE.md` and `docs/adr/0002-bitchat-mesh-module.md`). Edit copied files under `mesh/` only when needed, and record every change in the file's provenance header.
- **App:** namespace and applicationId `com.team27.mama`, label "Aló Mamá", minSdk 26, compileSdk 37 (the Compose BOM requires it), targetSdk 36. Jetpack Compose + Material3 through the Compose BOM. kotlinx-serialization is available (plugin + `kotlinx-serialization-json`). Unit tests are JUnit4 under `app/src/test/`.
- **App theme and navigation:** `app/src/main/java/com/team27/mama/ui/theme/` and navigation belong to ticket #5. See `app/UI_RULES.md`.
- **Server:** module `github.com/platanus-hack/platanus-hack-26-ve-team-27/server`, `go 1.24`, standard library `net/http` only. It listens on `$PORT` (default 8080). Register every route in `newMux()` in `server/cmd/server/routes.go`. Put packages under `server/internal/`.
- **Secrets:** never commit keys, keystores, `.env*` or `local.properties`. The secret scan hook in `scripts/build.sh` belongs to ticket #4.

### App UI

Before touching app screens, read `app/UI_RULES.md` (one theme file, Material3 components only, Spanish copy).
