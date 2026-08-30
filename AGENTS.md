This product is not live, no need to consider about compatible or migration issue.
Also go with the right and best approach and clean implementation.

## Frontend work starts from the Storybook

`ui/` Storybook is the single source of truth for how this product looks — 35
numbered screens, each with its own story and its design notes in that story's
Docs tab.

When touching anything under `ui/`:

1. **Open the Storybook first** (`cd ui && bun run storybook`). Read
   `Design/Introduction`, `Design/Screens` (screen → route → page index) and
   `Design/Contributing` before writing code.
2. **Work against the screen's story.** Each page story names the screen it
   ports and shows that screen's design notes in its Docs tab; measure the
   implementation against that story. Nothing gets built that has no screen.
3. **New visual element → its own file in `ui/src/components/ui/`** (the
   component + its CSS, e.g. `Button.tsx` + `Button.css`) **with a story**,
   not a one-off `style={{}}` inside a page.
4. **A new or changed page ships with its story** (`pageMeta({ screen, route,
   api })` plus fixtures built with `pb(Schema, …)`), covering the states that
   are awkward to reach in a browser: empty, error, viewer role.
5. `cd ui && bun run test` renders every story and fails if a fixture no longer
   answers the endpoint its page calls. Keep it green.
