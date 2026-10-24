# UI rules for the MAMÁ app

Read this before touching any screen in `app/`. These rules keep the look in one place so a palette or font change is a one-file edit.

## The one theme file

- `app/src/main/java/com/team27/mama/ui/theme/Theme.kt` holds every color, the font family, the typography and the shapes. It is the only file allowed to call `Color(...)`, create a `FontFamily`, or build a `ColorScheme`, `Typography` or `Shapes`.
- Wrap UI in `MamaTheme { }`. `MainActivity` already does this around the navigation shell, so screens inside it get the theme for free. `@Preview` functions wrap their screen in `MamaTheme` too.
- To change the palette, edit the color values in `Theme.kt`. To change the font, edit the single `MamaFontFamily` line.

## In screens

- Colors: `MaterialTheme.colorScheme.*` only (`primary`, `onSurface`, `errorContainer`, ...). No `Color(0x...)`, no `Color.Red`, no `colorResource`.
- Text: `MaterialTheme.typography.*` only (`headlineMedium`, `bodyLarge`, ...). No `fontSize = ...sp`, no `fontFamily`, no `FontWeight` overrides.
- Corners: `MaterialTheme.shapes.*` only, or the component's default shape.
- Spacing: plain `dp` values for padding and spacing are fine (use multiples of 4: 4, 8, 16, 24).
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
3. Add a `composable(Route.History.path) { HistoryScreen(onBack = { navController.popBackStack() }) }` entry inside the `NavHost`.
4. Navigate to it from another screen's callback with `navController.navigate(Route.History.path)`.

## Self-check before a PR

`grep -rn "Color(0x" app/src/main` must only list `Theme.kt`. Same for `FontFamily(` and `.sp`.
