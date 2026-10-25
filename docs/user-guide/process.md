# Process report

`ghx process` generates an HTML report that reads a repository's issues and
pull requests as a production line: items sit in stations, move between them,
and the report answers how much work sits where, how long it takes to flow
through, and whether the queues are growing.

Run `ghx cache` first so the report covers full history; like `stats`, it
serves data from the local cache and only falls back to the GitHub API when
nothing is cached.

## The two lines

- **Issues**: an open issue sits in the *Open* station; closing it moves it to *Closed*.
- **Pull requests**: a PR is *Draft* until it is marked ready for review, then
  *Ready*. Merging moves it to *Merged*; closing without merging moves it to
  *Discarded*, which counts as scrap rather than as a completion.

Cycle time is measured from creation to the completion station (close for
issues, merge for pull requests). Draft/ready history comes from cached
timeline events, so repositories cached before that data was collected show
their open PRs at the station their current state implies.

## What the report shows

- **Production line**: each station's inventory with the waiting time of the
  items currently there, and the moves observed in the window as arrows.
- **Cumulative flow**: one stacked band per active station per day, with
  cumulative completions on top; a widening band is a queue forming.
- **Bottlenecks**: where completed PR time went (draft versus ready), and each
  station's queue pressure (arrived, departed, net, and a weekly WIP
  sparkline). A filling queue receives items faster than it passes them on.
- **On the line now**: the aging list of every open issue and open PR, longest
  wait first (the rendered page caps the table; the JSON API lists them all).
- **Cycle time**: median, p90, and mean per line with a histogram.
- **Flow**: Little's law per line (average work in process, throughput, and
  the cycle time the queue depth implies), weekly arrivals versus completions,
  a Monte Carlo forecast of when today's items clear, and where the window's
  arrivals stand now.

## Usage

```bash
ghx cache                                       # populate the cache first
ghx process                                     # current repository, last 90 days
ghx process acme/widget acme/gadget --days 30   # several repositories
ghx process --days 180 -o flow-report.html      # longer window, custom output
```

## Flags

| Flag | Description |
| --- | --- |
| `--days` | Reporting window in days, 1-730 (default `90`) |
| `--output`, `-o` | Output HTML file path (default `process-report.html`) |

Repositories are given as positional `[HOST/]OWNER/REPO` arguments and default
to the current repository (or `--repo`).

## In the web UI

The same report is served by `ghx serve` at `/process`, computed from the
cache only; the Process button in the top bar opens it for the current
repository. The page carries a window form (a days input with an Apply
button) that reloads the report over the chosen window while keeping the
requested repositories. The numbers are also available as JSON at
`/api/process`:

```bash
curl -s "http://127.0.0.1:8080/process?repo=github.com/octocat/hello-world&days=30"
curl -s "http://127.0.0.1:8080/api/process?repo=github.com/octocat/hello-world" | jq '.issues.cycleTime'
```

| Parameter | Meaning |
| --- | --- |
| `repo` | `host/owner/repo` to report on; repeat for several repositories, omit for all cached ones |
| `days` | Reporting window in days, 1-730 (default `90`) |

An invalid `days` value answers `400`; a repository that is not cached fails
with `400` on the JSON endpoint and `500` on the HTML page.
