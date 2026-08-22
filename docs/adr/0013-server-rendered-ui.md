# The UI is server-rendered Go templates with htmx, not a single-page application

The web UI is `html/template` plus htmx, compiled into the binary via `embed.FS`, with JavaScript used only for the Trace graph. A React or Vue single-page application was rejected: the requirement is filterable tables that render fast and a graph that loads, and a second toolchain, build step and frontend skillset is disproportionate cost for a solo part-time build that is simultaneously writing three configuration parsers.

Recorded because it is a deliberate deviation from the obvious path — a future contributor will assume an SPA was an oversight and offer to "fix" it. The interaction ceiling is genuinely lower; that trade was made knowingly.
