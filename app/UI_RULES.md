# UI rules for the MAMÁ app

Read this before touching any screen in `app/`. These rules keep the look in one place so a palette or font change is a one-file edit.

## The one theme file

- `app/src/main/java/com/team27/mama/ui/theme/Theme.kt` holds every color, the font family, the typography, the shapes, the spacing scale (`MamaSpacing`) and the wordmark (`MamaWordmark`, `MamaMark`). It is the only file allowed to call `Color(...)`, create a `FontFamily`, set a `FontWeight`, or build a `ColorScheme`, `Typography` or `Shapes`.
- Wrap UI in `MamaTheme { }`. `MainActivity` already does this around the navigation shell, so screens inside it get the theme for free. `@Preview` functions wrap their screen in `MamaTheme` too.
- To change the palette, edit the color values in `Theme.kt`. To change the font, edit the single `MamaFontFamily` line.

## The identity (#8)

- **Palette:** from the event logo (`project-logo.png`): charcoal `#181918` and chartreuse `#EFFE00`. Dark mode is the logo itself (chartreuse `primary` on charcoal). Light mode uses a deep olive of the same hue for `primary` (chartreuse is unreadable on white) and a soft lime for `primaryContainer`. `secondary` is warm graphite, `tertiary` a muted periwinkle. Every text pair passes WCAG AA in both modes.
- **Red is for errors only** (`error`, `errorContainer`). MAMÁ must look like a family app to whoever is next to the user, never like a panic button. Do not use `error` for emphasis.
- **Type:** Lexend (SIL OFL 1.1, bundled in `res/font/`, license in `assets/licenses/`), weights 400 / 500 / 700 only. Headlines and display are bold, titles and labels medium, body regular. Pick a style by role; do not override the weight in a screen.
- **Wordmark:** use `MamaWordmark()` (big, e.g. a splash or empty state) or `MamaWordmark(compact = true)` (inside a `CenterAlignedTopAppBar` title, as the two reference screens do). Use `MamaMark(size)` when you need the mark alone: it is Mamá's face (`res/drawable-nodpi/mama_mark.webp`, a head crop of the same riso illustration `MamaCharacter` animates), clipped to a circle. Never re-type "MAMÁ" with your own style or draw your own mark. The event's ↗ arrow (`ic_mama_mark.xml`) stays only as the notification small icon, which must be a single-colour vector.
- **Launcher icon:** `res/mipmap-anydpi-v26/ic_launcher*.xml`: the foreground is Mamá as a bitmap per density (`res/mipmap-*/ic_launcher_foreground.png`, the whole bust with the grain smoothed and her circle knocked out, hair inside the 66 dp safe zone) over `res/drawable/ic_launcher_background_mama.xml` (solid brand chartreuse), plus `ic_launcher_monochrome.xml` (her silhouette) for themed icons. Sources and the comparison sheet live in `mama-personaje/logo/v2/`. The launcher draws it outside the app, so it cannot read `Theme.kt`: it is the one place with hex colors outside the theme file. Regenerate them from `mama-personaje/logo/v2/` if the illustration changes. The old arrow vectors (`ic_launcher_foreground.xml`, `ic_launcher_background.xml`) remain only for the home-screen widget layout. The window theme (`res/values/themes.xml` and `values-night/`) only sets what Android draws before Compose (light or dark launch frame); it holds no colors.
- **Icons:** the project has no Material icons dependency. Single-color vectors live in `res/drawable/` with `android:fillColor="@android:color/white"`, and screens tint them through `Icon(painterResource(...))`, which takes `LocalContentColor` (a theme token) by default.

## Reference screens

`SetupScreen.kt` and `TriggerConfirmationScreen.kt` are the reference for every other screen. Copy their structure:

- `Scaffold` with `CenterAlignedTopAppBar(title = { MamaWordmark(compact = true) })`.
- Side margin `MamaSpacing.screen` (24 dp), gaps `MamaSpacing.m` (16 dp) between blocks and `MamaSpacing.s` (8 dp) inside a block.
- One `headlineMedium`/`displaySmall` sentence that says what is happening, one `bodyLarge` line in `onSurfaceVariant` that says what to do.
- Grouped rows: a `Card` on `surfaceContainerLow` holding `ListItem`s split by `HorizontalDivider(color = outlineVariant)`. Section labels are `titleSmall` in `primary`.
- One primary `Button` per screen. Secondary or "undo" actions are a `TextButton` (the trigger screen's "Fue sin querer" stays discreet on purpose).
- Step state: done = `primary` circle with the check, running = `CircularProgressIndicator`, pending = `surfaceContainerHighest` circle with an `outline` border, failed = `errorContainer` row with an `error` badge. Merge each row for TalkBack (`Modifier.semantics(mergeDescendants = true) { stateDescription = ... }`).
- Button labels keep the button's own style (`labelLarge`); do not pass a `style` to a button's `Text`.
- A bottom action bar pads for the keyboard and the navigation bar together: `Modifier.windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Bottom))` (the activity uses `adjustResize` with edge-to-edge).
- The trigger confirmation screen can be read by a bystander: neutral words only (no alerta, emergencia, peligro), and never explain the duress phrase or what the family receives there.

## In screens

- Colors: `MaterialTheme.colorScheme.*` only (`primary`, `onSurface`, `errorContainer`, ...). No `Color(0x...)`, no `Color.Red`, no `colorResource`.
- Text: `MaterialTheme.typography.*` only (`headlineMedium`, `bodyLarge`, ...). No `fontSize = ...sp`, no `fontFamily`, no `FontWeight` overrides.
- Corners: `MaterialTheme.shapes.*` only, the component's default shape, or `CircleShape` for round badges and avatars.
- Spacing: prefer the `MamaSpacing` scale (`xs` 4, `s` 8, `m` 16, `l` 24, `xl` 32, `xxl` 48, `screen` 24). Plain `dp` values are fine for component sizes (an icon, a badge), in multiples of 4.
- Components: Material3 only (`androidx.compose.material3.*`): `Scaffold`, `TopAppBar`, `Button`, `OutlinedButton`, `TextButton`, `Card`, `ListItem`, `Switch`, `OutlinedTextField`, `AlertDialog`, and so on. Do not import `androidx.compose.material.*` (Material2) and do not draw custom buttons from `Box` + `clickable`.
- If a component needs a non-default color, pass a theme token through its `*Defaults` helper, for example `CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer)`.
- A screen needs a color or text style the theme does not have? Add it to `Theme.kt` (map it to a Material3 role), never inline in the screen.

## Copy

- All user-facing text is Spanish (Venezuela), informal "tú". Example: "Continuar", "Mamá te va a llamar".
- Code, comments and identifiers stay in English.

## Where things go

- Screens: `app/src/main/java/com/team27/mama/ui/screens/<Name>Screen.kt`. One public `@Composable fun <Name>Screen(...)` per file; it takes navigation callbacks (`onContinue: () -> Unit`) instead of a `NavController`.
- Routes and the nav shell: `app/src/main/java/com/team27/mama/ui/navigation/MamaNavHost.kt`.

## Adding a screen

1. Create `ui/screens/<Name>Screen.kt` following the rules above, with a `@Preview` wrapped in `MamaTheme`.
2. Add a value to the `Route` enum in `MamaNavHost.kt`, for example `History("history")`.
3. Add a `composable(Route.History.path) { ... }` entry inside the `NavHost`. For a back callback copy the guarded form already there: `if (navController.previousBackStackEntry != null) navController.popBackStack()`.
4. Navigate to it from another screen's callback with `navController.navigate(Route.History.path) { launchSingleTop = true }` (a double tap must not push it twice).

## Self-check before a PR

This must print nothing (it skips `Theme.kt`, the only file allowed to match):

```bash
grep -rnE 'Color\(|Color\.[A-Z]|colorResource|FontFamily|FontWeight|fontSize|[0-9]\.sp\b|letterSpacing|TextStyle\(|RoundedCornerShape|androidx\.compose\.material\.' app/src/main/java | grep -v ui/theme/Theme.kt
```
