# Ticket profile

<!--
Copy this file to `.agents/ticket-profile.md` at your repository root and fill
it in. The `ticket` skill reads it before every run, and it wins over anything
the skill would infer. Delete any section you don't need; the skill infers the
missing ones from AGENTS.md, README.md and the repository's files.
-->

## Tracker

GitHub issues on `<owner>/<repo>`. Issues ready for an agent carry the
`ready-for-agent` label.

<!-- Or: Linear team `ABC`; GitHub issues are not used. -->

## Bootstrap

Run in a fresh worktree before anything else:

```bash
npm ci
```

## Checks

Must pass before every push:

```bash
npm run lint
npm run typecheck
npm test
```

<!-- Say so when a check doesn't exist yet, e.g. "No test suite yet." -->

## Constraints

- The app needs `DATABASE_URL`; without it, verify manually and say so in the
  PR body.
- Never run migrations against the shared database; commit the migration file
  and leave applying it to a human.
- The dev server listens on port 3000; only the main process may start it.

## Parallel slices

- An API route plus the frontend component that calls it.
- A schema change plus the queries that use it.

## App

<!--
Used for PR screenshots of UI changes. Delete this section if the repository
has no UI.
-->

- Start: `npm run dev`
- URL: `http://localhost:3000`
- Login: sign in as `demo@example.com` / `demo` (seeded by `npm run seed`)
- Viewports: desktop `1280,800`, mobile `390,844`
