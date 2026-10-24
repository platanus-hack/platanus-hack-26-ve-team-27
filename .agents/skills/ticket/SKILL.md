---
name: ticket
description: Take a GitHub or Linear issue end to end in its own Herdr tab using a selected implementation CLI. Use when the user invokes or asks to run the ticket workflow.
---

Request: $ARGUMENTS

You are the ticket orchestrator. The CLI running this command and the CLI that
implements the ticket are independent.

This workflow owns branch and worktree creation and requires the
implementation process to commit, push, and open the pull request. If the
repository also ships a standalone ticket workflow (for example an
`implement-ticket` skill that creates its own worktree, or a `deliver` skill),
never invoke it from this workflow, and never let the process you launch
invoke it.

When loaded as a skill rather than a command template, `$ARGUMENTS` remains
literal. In that case, parse the issue and implementation agent from the
user's current request instead.

Parse the request into:

- `issue`: the issue identifier or URL
- `implementation_agent`: the value after `--agent`; it must be one of
  `claude`, `codex`, `opencode`, `agy`, `claudex`, or `pi`
- `implementation_model` (optional): the value after `--model`; the model the
  implementation CLI runs on, in that CLI's own model syntax (for example
  `opus` or `claude-opus-5-5` for `claude`, `provider/model` for `opencode`)
- `review_model` (optional): the value after `--review-model`; the model the
  independent reviewer subagent runs on, in the same syntax
- `implementation_effort` (optional): the value after `--effort`; the
  implementer's reasoning effort (`low`, `medium`, `high`, `xhigh`, `max`).
  Only `claude` takes it; for other kinds, ignore it and say so
- `review_effort` (optional): the value after `--review-effort`; the reviewer
  subagent's reasoning effort, same levels. Only `claude` takes it

The tracker is inferred from the input:

- A Linear identifier such as `ABC-123` or a `linear.app/...` URL → Linear
- A GitHub issue number or a `github.com/...` URL → GitHub
- An explicit prefix, `linear:ABC-123` or `github:123`, when the input could
  be ambiguous

When the input is still ambiguous, use the tracker named in the repository
profile (see "Repository profile" below). Without one, a bare number is a
GitHub issue on the repository's `origin` remote.

If `--agent` is omitted, check `TICKET_AGENT` with `printenv TICKET_AGENT`.
Use it only when it is one of the supported values. If neither source provides
a valid value, ask the user which implementation agent to use. Never infer it
from the CLI currently running this command.

If `--model` is omitted, check `TICKET_MODEL`; if `--review-model` is omitted,
check `TICKET_REVIEW_MODEL`; if `--effort` is omitted, check `TICKET_EFFORT`;
if `--review-effort` is omitted, check `TICKET_REVIEW_EFFORT`.
When neither source gives a value, use these defaults:

| `implementation_agent` | `implementation_model` | `implementation_effort` | `review_model` | `review_effort` |
| --- | --- | --- | --- | --- |
| `claude` | `claude-opus-5-5` | `medium` | `claude-sonnet-5-5` | `medium` |
| `claudex` | `gpt-5.6-sol` | unset | same as implementer | unset |
| any other | CLI default | unset | same as implementer | unset |

An explicit flag or environment value always overrides the default. Do not ask
the user for a model that was not given, and never substitute a model the user
did not name. Pass the value through verbatim; the implementation CLI
validates it, and if the CLI rejects it, stop and report the error instead of
falling back silently.

Examples:

- `ticket 42 --agent codex` (GitHub)
- `ticket ABC-123 --agent codex` (Linear)
- `ticket https://github.com/acme/widgets/issues/42 --agent opencode`
- `ticket https://linear.app/acme/issue/ABC-123 --agent agy`
- `ticket 42 --agent pi`
- `ticket 42 --agent claude` (Opus 5.5 implementer, Sonnet 5.5 reviewer, both at medium effort)
- `ticket 42 --agent claude --effort high --review-model claude-opus-5-5`
- `ticket 42 --agent opencode --model openai/gpt-5.6 --review-model anthropic/claude-sonnet-5-5`

## Repository profile

This skill is repository-agnostic. Everything project-specific comes from the
repository you are running in. Before step 1, build a profile with these
fields, in this order of precedence:

1. `.agents/ticket-profile.md` at the repository root, if it exists. It is
   written by the team and wins over anything you infer. The skill folder
   ships `profile-template.md` as the format.
2. The repository's agent instructions (`AGENTS.md`, `CLAUDE.md`,
   `CONTRIBUTING.md`) and `README.md`.
3. Inference from the files in the repository, as described per field.

Fields:

- **Tracker.** GitHub or Linear, and for GitHub any label that marks issues
  ready for an agent. Default: GitHub on `origin`.
- **Bootstrap.** The command that makes a fresh worktree ready to build and
  push. Use `tools/bootstrap-worktree.sh` when it exists. Otherwise infer it
  from the manifests at the root: `pnpm-lock.yaml` → `pnpm install
  --frozen-lockfile`, `yarn.lock` → `yarn install --frozen-lockfile`,
  `package-lock.json` → `npm ci`, `bun.lock`/`bun.lockb` → `bun install`,
  `uv.lock` → `uv sync`, `poetry.lock` → `poetry install`,
  `requirements.txt` → create a virtualenv and `pip install -r
  requirements.txt`, `go.mod` → `go mod download`, `Cargo.toml` → `cargo
  fetch`, `*.sln`/`*.csproj` → `dotnet restore`, `pom.xml` → `mvn -q
  dependency:resolve`, `build.gradle*` → `./gradlew dependencies`,
  `Gemfile.lock` → `bundle install`, `composer.lock` → `composer install`. A
  monorepo may need several. When nothing matches, there is no bootstrap.
- **Checks.** The commands that must pass before a push: lint, type check,
  build, tests. Take them from the documented instructions, then from
  `package.json` scripts, a `Makefile`/`justfile`/`Taskfile`, the CI workflow
  under `.github/workflows/`, or the language's standard build and test
  commands. Record which checks exist and which do not (for example "no test
  suite yet").
- **Constraints.** Anything the implementer must not do: shared databases,
  migrations to leave unapplied, services that need credentials, fixed ports
  that collide when two processes start the app in one worktree, generated
  files not to edit.
- **Parallel slices.** Examples of independent slices in this codebase (for
  example "API route plus the client that calls it"), used in step 7.
- **App.** How to run the app locally for screenshots: the start command,
  the URL it serves, how to log in if a page needs it, and the viewports if
  not desktop `1280,800` and mobile `390,844`. Take it from the README or the
  `dev`/`start` script. When the repository has no UI, there is nothing to
  record.

State the profile in a few lines before step 1, naming where each field came
from (profile file, instructions, or inference). If a field that step 2 or
step 7 needs is inferred with low confidence, say so; do not stop for it.

## Steps

Before making changes, state which implementation agent will receive the
ticket, which tracker the ticket came from, and the implementation and review
models (or "CLI default" / "same as implementer" when unset). Then:

1. Pull the issue and obtain its canonical identifier, title, URL, and full
   requirements.

   For GitHub, run
   `gh issue view <issue> --comments --json number,title,url,body,comments,labels`
   to pull the issue and obtain its canonical issue number. Read the comments
   and any "Blocked by" section or blocking relation; stop and report if a
   blocker is still open. Use `#<issue-number>` as the `<ticket-ref>`. If the
   issue cites requirement IDs or documents in the repository, include the
   cited text in the task.

   For Linear, fetch the issue with the Linear MCP `get_issue` tool using the
   identifier. Read its comments with `list_comments` when they may affect the
   requested behavior. Include relations when blockers or duplicates matter.
   Use the identifier as `<ticket-ref>`.
2. Determine the repository's main branch with
   `git symbolic-ref --short refs/remotes/origin/HEAD` (falling back to
   whichever of `main` or `master` exists on `origin`). Fetch that branch from
   `origin`, then create a ticket branch and worktree from the fresh
   `origin/<main-branch>`. Never base the worktree on whichever branch happens
   to be checked out.
   The worktree must live at `<repo-root>/.worktrees/<slug>` where `<slug>` is
   a short, lowercase, hyphenated slug derived from the ticket identifier and
   title, and the branch is `ticket/<slug>`. The `.worktrees` container lives
   inside the repository root and must be listed in the repository's
   `.gitignore`; if it is not gitignored, add it (or stop and report if you
   cannot). Never create worktrees as siblings of the repository or anywhere
   else outside `<repo-root>/.worktrees`. If the branch or worktree already
   exists, stop for explicit direction — never delete, reset, clean, or
   repurpose an existing one.

   ```bash
   mkdir -p <repo-root>/.worktrees
   git worktree add -b <branch-name> <repo-root>/.worktrees/<slug> origin/<main-branch>
   ```

   Record the worktree path exactly as Git prints it. On Windows, Git Bash
   shows `/c/...` while Git reports `C:/...`, and the step 7 check compares
   against Git's form. Use this value as `<worktree_path>` from here on,
   including in `TICKET_WORKTREE`:

   ```bash
   git -C <repo-root>/.worktrees/<slug> rev-parse --show-toplevel
   ```

   A new worktree has no installed dependencies or build output, and a
   pre-push hook may refuse to push without them. Run the profile's bootstrap
   inside the new worktree before you launch anything there. If it fails, stop
   and report the error:

   ```bash
   (cd <worktree_path> && <bootstrap command>)
   ```
3. Run `herdr pane current` and take the `workspace_id` from its output.
4. Create a background tab rooted at the worktree, carrying the orchestration
   markers and the skill isolation the implementation process needs:

   ```bash
   herdr tab create --workspace <workspace_id> --cwd <worktree_path> \
     --label "<ticket-ref>" --no-focus \
     --env TICKET_ORCHESTRATED=1 \
     --env TICKET_WORKTREE=<worktree_path> \
     --env TICKET_BRANCH=<branch-name> \
     --env 'OPENCODE_CONFIG_CONTENT={"permission":{"skill":{"*":"allow","implement-ticket":"deny","deliver":"deny"}}}'
   ```

   Set these on the tab only. Never export them into the shell running this
   command: the tab is created after `git worktree add` succeeds, so no
   pre-worktree environment can inherit the markers by accident.

   `OPENCODE_CONFIG_CONTENT` is process-local skill isolation. A standalone
   ticket workflow conflicts with this orchestrator — it treats the current
   worktree as a launch point and creates another one, or forbids the commit,
   push, and pull request this workflow requires. Claude-specific
   `disable-model-invocation` metadata does not stop OpenCode from invoking
   such a skill, so deny it by permission instead; denying a skill the
   repository does not have is harmless. If the repository has another
   standalone ticket workflow under a different name, add it to the deny list.
   Every other skill stays available, including subproject skills registered
   in the repository's `opencode.json`. Pass the variable for every
   implementation agent; agents other than OpenCode ignore it.

   When `implementation_agent` is `opencode` and `review_model` is set, also
   define the reviewer subagent in the same variable so OpenCode pins its model:

   ```json
   {"permission":{"skill":{"*":"allow","implement-ticket":"deny","deliver":"deny"}},
    "agent":{"ticket-reviewer":{"mode":"subagent","model":"<review_model>",
      "description":"Independent reviewer for the ticket diff; gets no implementation context."}}}
   ```

5. Take the new tab's initial pane ID from the creation result. If necessary,
   identify it with `herdr pane list --workspace <workspace_id>` by matching the
   new tab label and worktree path. Do not confuse the tab ID with the pane ID.
6. Start the selected CLI in that pane.

   When `implementation_agent` is `claudex`, source the shared Claudex
   environment into the target pane so all provider variables, model-family
   mappings, and subagent routing are inherited by the Claude process. Then
   start with `--kind claude` and the explicit model after `--`:

    `herdr pane run <pane_id> "source \"${CLAUDEX_ENV:-$HOME/.config/claudex/env.sh}\""`

    `herdr agent start "ticket-<ticket-ref>" --kind claude --pane <pane_id> -- --model gpt-5.6-sol`

   If the Claudex environment file does not exist, stop and tell the user
   instead of starting a plain `claude` process in its place. The shared
   environment maps Opus, Sonnet, Haiku, the default model, and subagents to
   `gpt-5.6-sol`. Start a fresh pane when switching between `claude` and
   `claudex` agents: a pane previously used for one pathway retains its
   exported provider variables.

   When `implementation_agent` is `opencode`, start it with the `--auto` flag:

    `herdr agent start "ticket-<ticket-ref>" --kind opencode --pane <pane_id> -- --auto`

   Arguments for the underlying CLI are positional and must follow a `--`
   separator; there is no `--agent-args` flag.

   When `implementation_agent` is `pi`, start it directly with the `pi` Herdr
   kind. Do not pass an `--auto` flag; `herdr agent prompt` supplies the task
   after the interactive pi process is ready:

    `herdr agent start "ticket-<ticket-ref>" --kind pi --pane <pane_id>`

   For all other agents, use the agent value directly as the Herdr kind:

    `herdr agent start "ticket-<ticket-ref>" --kind <implementation_agent> --pane <pane_id>`

   **Implementation model.** When `implementation_model` is set, append
   `--model <implementation_model>` to the CLI arguments after `--` (add the
   `--` if the start command above has none). For example:

    `herdr agent start "ticket-<ticket-ref>" --kind claude --pane <pane_id> -- --model claude-opus-5-5 --effort medium`

    `herdr agent start "ticket-<ticket-ref>" --kind opencode --pane <pane_id> -- --auto --model <implementation_model>`

   For `claude`, also append `--effort <implementation_effort>` when it is set.
   For `claudex`, the value replaces `gpt-5.6-sol`, and it must be a model the
   Claudex environment routes. If the selected CLI has no model flag, stop and
   tell the user rather than starting it on a model they did not choose.

   **Review model (Claude kinds).** When `implementation_agent` is `claude` or
   `claudex` and `review_model` is set, also define the reviewer subagent on
   the command line so its model is pinned by configuration, not just by the
   prompt (omit `--effort` when `implementation_effort` is unset, and the
   reviewer's `"effort"` key when `review_effort` is unset):

    `-- --model <implementation_model> --effort <implementation_effort> --agents '{"ticket-reviewer":{"description":"Independent reviewer for the ticket diff","prompt":"Review only the requirements and diff you are given. Report findings; never edit files.","model":"<review_model>","effort":"<review_effort>"}}'`

   For other kinds, the review model is carried by the prompt in step 7.

7. Hand it the complete task:

   `herdr agent prompt "ticket-<ticket-ref>" "<full task>"`

   The full task must include the issue URL, the full requirements and any
   relevant comments, branch/worktree context, and explicit instructions to
   implement the ticket, add or update tests, run the relevant checks,
   self-review the final diff, fix discovered problems, commit the work, and
   create the PR with `gh pr create`. The PR title must end with
   ` (<ticket-ref>)` — for example, if the ticket is `ABC-123`, the PR title
   would be "feat: recipe rotation (ABC-123)"; if the ticket is issue 42, the
   PR title would be "feat: recipe rotation (#42)". Follow the repository's
   commit and PR conventions when it documents them.

   The prompt must list the profile's checks as the commands that must pass,
   say which checks do not exist yet, and repeat every profile constraint.
   When a criterion cannot be verified automatically (no test suite, a
   service or credential that is not available), the implementer verifies it
   manually where it can, says so in the PR body, and never invents passing
   evidence.

   When the ticket changes anything a user sees, the prompt must also require
   screenshots in the PR, taken the cheapest way:

   - **Playwright's command line, not browser tools.** Take each shot with
     one shell command, `npx playwright screenshot --viewport-size=<w,h>
     <url> <file>.png`. Add `--wait-for-selector` when the page renders
     late. For pages behind a login, save the session once with a short
     Playwright script (`context.storageState({ path })`) and pass
     `--load-storage <file>`; use a script for the shot itself only when
     clicks are needed to reach the state. Never use an MCP browser tool,
     which returns a page snapshot to the model on every action. If
     Playwright's browser is missing, install it once with `npx playwright
     install chromium`. Look at a shot only when you need to confirm it shows
     the right state.
   - **Two viewports.** Shoot every state at desktop (`1280,800`) and mobile
     (`390,844`), unless the profile names other viewports.
   - **Before, then after.** The worktree starts at a fresh
     `origin/<main-branch>`, so take the "before" shots of a changed screen
     first, before editing any code. Take the "after" shots once the
     acceptance criteria pass. A new screen gets only "after" shots. Only the
     main process starts the app, using the profile's run command and URL.
   - **Commit them and pin the URL.** Save the shots as
     `docs/screenshots/<slug>/<state>-<desktop|mobile>.png` (the worktree
     slug from step 2), commit and push them before `gh pr create`, and embed
     each one with a URL pinned to the pushed commit, so it renders in a
     private repository:

     ```html
     <img src="https://github.com/<owner>/<repo>/blob/<head-sha>/<path>?raw=true" alt="<state>, <viewport>" width="<640 for desktop, 320 for mobile>" />
     ```

     A relative image path resolves against the base branch and renders
     nothing. Lay the shots out as a table with before and after side by
     side. If the app cannot run (missing services or credentials), say so
     in the PR body instead of the screenshots; never fake one.

   The prompt must also authorize parallel delegation inside the worktree,
   because the implementation CLI will otherwise work sequentially by
   default:

   - **Parallel delegation.** Before implementing, split the ticket into
     slices that touch disjoint files. When there are two or more
     substantial slices (use the profile's examples, or ones such as a
     backend change plus the client that consumes it, several components of
     one screen, or the implementation plus a docs update), delegate them to
     parallel subagents working in this same worktree, each with its own file
     set and the ticket context it needs. Sequential work remains the
     default: a subagent starts cold, so a small or single-package ticket is
     faster without delegation.
   - Only the main implementation process runs `git` writes (commit, push),
     dependency installs, the bootstrap command, schema migrations, and the
     full check suite. Subagents edit their own files and run only the unit
     tests or build for what they touch; they never start the app or
     end-to-end suites, which usually listen on fixed ports and collide inside
     one worktree, and they never touch a database or other shared service.
     The main process integrates the slices, runs the full checks once, and
     resolves any overlap itself.
   - Subagents inherit the orchestration contract below: they must never
     create a branch or worktree, and never invoke a standalone ticket
     workflow. If the implementation CLI cannot spawn subagents, implement the
     slices sequentially and say nothing more about it.

   The prompt must also require two gates after the work is committed and
   before `gh pr create`:

   - **Independent review.** Spawn a fresh reviewer subagent that gets no
     implementation context: only the ticket requirements, the repository's
     standards, and the diff against `origin/<main-branch>`. Use the
     repository's `code-review` skill when it exists. The implementation
     agent fixes every finding it confirms, re-runs the relevant checks, and
     commits the fixes; it records rejected findings and why. If the
     implementation CLI cannot spawn subagents, run the review as a separate
     pass that rereads only the requirements and the diff, and say so in the
     PR body.

     When `review_model` is set, the prompt names it and requires the
     reviewer to run on it: use the `ticket-reviewer` subagent where step 4
     or step 6 defined one, otherwise pass the model through the CLI's own
     subagent model option. If the CLI cannot pin a subagent's model, run the
     review on whatever model is available and record the model actually
     used in the PR body; never claim the requested one.
   - **Acceptance criteria.** Check every acceptance criterion of the ticket
     one by one against the final commit and record each as pass or fail
     with concrete evidence: a test name, a command and its output, or a
     screenshot. Never mark a criterion as passing without evidence. If the
     ticket has no explicit criteria, derive them from its requirements and
     say so. Open the PR as ready only when every criterion passes; otherwise
     open it with `gh pr create --draft` and state which criteria fail and
     why.

   The PR body is the record of what was done; there is no separate write-up
   file. Keep it short and scannable. When the repository has
   `.github/pull_request_template.md`, copy it, fill every section, remove
   its comments and any optional section with nothing to say, and pass it
   with `gh pr create --body-file`; `gh` does not apply the template on its
   own. Without a template, the body must include, in this order:

   - **Why:** the problem or request, in one or two lines.
   - **Root cause:** what was actually wrong, or "n/a (new feature)".
   - **Changed:** the files or areas touched, each with what changed in a
     few words.
   - **Acceptance criteria:** a table of criterion, result, and evidence.
   - **Screenshots:** the before/after table, or "n/a (no UI change)".
   - **Rejected review findings:** each finding the implementer rejected,
     with the reason. Omit the section when none were rejected; fixed
     findings are already in the commits.
   - **Watch out:** trade-offs, limits, or deliberate follow-ups a later
     ticket must know. Omit the section when there are none.
   - **Models:** the implementation model, and the model that reviewed the
     diff (for example "reviewed by claude-sonnet-5-5"), which also shows the
     review gate ran.

   Do not rely on the implementation CLI having access to the tracker's MCP
   server. Paste the full issue description and any relevant comments into
   the prompt so the agent has everything it needs.

   The prompt must also state the orchestration contract:

   - This workflow already created the branch and the worktree. Do not create
     another branch or worktree, and do not invoke standalone ticket workflow
     skills; this process owns commit, push, and pull-request delivery
     itself.
   - Verify the prepared workspace before implementing, and stop instead of
     creating or guessing another worktree if any check fails:

     ```bash
     test "$TICKET_ORCHESTRATED" = 1
     test "$(git rev-parse --show-toplevel)" = "$TICKET_WORKTREE"
     test "$(git branch --show-current)" = "$TICKET_BRANCH"
     git worktree list --porcelain
     ```

     `git worktree list --porcelain` must contain an entry pairing
     `TICKET_WORKTREE` with `TICKET_BRANCH`.
   - The orchestrator already ran the bootstrap. After you change a lockfile
     or dependency manifest, run the bootstrap again. Every check must pass
     before every push. If a pre-push hook reports missing or stale
     dependencies, run the install command it prints and push again. Never
     push with `--no-verify`.

   The prompt is supporting context, not the proof. The environment markers
   and the skill isolation from step 4 are what make a configuration
   regression fail safely instead of producing a nested worktree.
8. Tell the user the implementation agent, implementation model, review model,
   tab label, branch name, and worktree path. Do not babysit the
   implementation agent here; the user can inspect live ticket status with:

   `herdr tab list --workspace <workspace_id>`
