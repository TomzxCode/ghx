package processreport

const reportTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Process report: {{.Repos}}</title>
<style>
:root {
  --bg: #f6f7f9; --fg: #1a202c; --muted: #64748b; --card: #ffffff;
  --border: #e2e8f0; --accent: #2563eb;
}
@media (prefers-color-scheme: dark) {
  :root { --bg: #12141a; --fg: #e5e9f0; --muted: #8b94a7; --card: #1b1f29;
          --border: #2a3140; --accent: #60a5fa; }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg);
       font: 15px/1.5 -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
.wrap { max-width: 1000px; margin: 0 auto; padding: 24px 16px 64px; }
a { color: var(--accent); text-decoration: none; }
a:hover { text-decoration: underline; }
h1 { font-size: 22px; margin: 0; }
h2 { font-size: 17px; margin: 0 0 12px; }
h3 { font-size: 13px; margin: 18px 0 8px; color: var(--muted); text-transform: uppercase; letter-spacing: 0.04em; }
.meta { color: var(--muted); font-size: 13px; margin-top: 4px; }
section { background: var(--card); border: 1px solid var(--border); border-radius: 10px;
          padding: 18px; margin-top: 16px; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 10px; }
.card { background: var(--bg); border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; }
.card .value { font-size: 20px; font-weight: 600; }
.card .label { font-size: 12px; color: var(--muted); margin-top: 2px; }
.two-col { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(320px, 100%), 1fr)); gap: 18px; }
.line-diagram { display: flex; flex-wrap: wrap; align-items: stretch; gap: 6px; }
.station { flex: 1 1 130px; min-width: 130px; border: 1px solid var(--border);
           border-left: 4px solid var(--station-color, var(--muted));
           border-radius: 8px; padding: 8px 10px; background: var(--bg); }
.station .name { font-size: 12px; color: var(--muted); }
.station .count { font-size: 20px; font-weight: 600; }
.station .ages { font-size: 12px; color: var(--muted); }
.arrow { display: flex; flex-direction: column; justify-content: center; align-items: center;
         color: var(--muted); font-size: 12px; min-width: 52px; gap: 2px; }
.arrow .head { font-size: 16px; letter-spacing: -2px; }
.chart { width: 100%; height: auto; }
.chart .tick { fill: var(--muted); font-size: 11px; }
.chart .grid { stroke: var(--border); stroke-width: 1; }
.chart .axis { stroke: var(--muted); stroke-width: 1; }
.chart-empty { color: var(--muted); font-size: 13px; padding: 12px 0; }
.legend { display: flex; flex-wrap: wrap; gap: 14px; font-size: 12px; color: var(--muted); margin-top: 8px; }
.legend .dot { display: inline-block; width: 10px; height: 10px; border-radius: 2px;
               margin-right: 5px; vertical-align: -1px; }
.dot { display: inline-block; width: 10px; height: 10px; border-radius: 2px; vertical-align: -1px; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
/* On narrow screens a wide table scrolls inside its card instead of
   stretching the page; the stats report uses the same rule. */
table { display: block; overflow-x: auto; }
th, td { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--border); }
th { color: var(--muted); font-weight: 500; font-size: 12px; }
td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
.badge { display: inline-block; padding: 1px 8px; border-radius: 999px; font-size: 12px;
         border: 1px solid var(--border); color: var(--muted); }
.badge.filling { color: #b45309; border-color: #b45309; }
.badge.draining { color: #16a34a; border-color: #16a34a; }
.badge.steady { color: var(--muted); }
.spark { width: 96px; height: 24px; vertical-align: middle; }
.share-bar { display: flex; height: 18px; border-radius: 6px; overflow: hidden;
             border: 1px solid var(--border); margin: 8px 0 12px; }
.share-seg { display: block; height: 100%; }
.note { color: var(--muted); font-size: 12px; margin-top: 10px; }
.window-form {
  display: flex; flex-wrap: wrap; gap: 0.4rem; align-items: center;
  margin: 0.25rem 0 0.1rem; font-size: 0.8rem; color: var(--muted);
}
.window-form label { font-size: 0.8rem; }
.window-form input {
  background: var(--card); color: var(--fg); border: 1px solid var(--border); border-radius: 6px;
  padding: 0.25rem 0.4rem; font-size: 0.8rem;
}
.window-form input:focus { outline: none; border-color: var(--accent); }
.window-form #window-days { width: 4.5em; }
.window-form button {
  background: var(--card); color: var(--fg); border: 1px solid var(--border); border-radius: 6px;
  padding: 0.25rem 0.65rem; font-size: 0.8rem; cursor: pointer;
}
.window-form button:hover { border-color: var(--muted); }
@media (prefers-color-scheme: dark) { .window-form input { color-scheme: dark; } }
@media (max-width: 640px) { .arrow { min-width: 36px; } }
</style>
</head>
<body>
<div class="wrap">
  <h1>Process report</h1>
  <div class="meta">{{.Repos}} &middot; {{if .Range}}{{.Range}}{{else}}last {{.WindowDays}} days{{end}} &middot; generated {{.GeneratedAt.Format "Jan 2, 2006 15:04 UTC"}} &middot; {{.TotalOpen}} items on the line now</div>
  {{if .Interactive}}
  <form class="window-form" method="get" action="/process">
    {{if not .AllRepos}}{{range .RepoIDs}}<input type="hidden" name="repo" value="{{.}}">
{{end}}{{end}}
    <label for="window-from">From</label>
    <input type="date" id="window-from" name="from" value="{{.FromValue}}" autocomplete="off">
    <label for="window-to">To</label>
    <input type="date" id="window-to" name="to" value="{{.ToValue}}" autocomplete="off">
    <label for="window-days">Window</label>
    <input type="number" id="window-days" name="days" min="1" max="730" value="{{.DaysValue}}" placeholder="all" autocomplete="off">
    <span>days</span>
    <button type="submit">Apply</button>
  </form>
  {{end}}
  {{if .Interactive}}<div class="note">Set From or To for an explicit range (an empty From starts at the first item, an empty To ends today). With the dates cleared, the window field sets a trailing window in days; with everything empty the report covers all available history.</div>{{end}}

  <section>
    <h2>Production line</h2>
    <h3>{{.Issues.Label}}</h3>
    {{.Issues.Diagram}}
    <h3>{{.PRs.Label}}</h3>
    {{.PRs.Diagram}}
    <div class="note">Box counts are the items sitting in each station now. Arrow counts are the
    moves observed in the window; the draft/ready arrow shows both directions.</div>
  </section>

  <section>
    <h2>Cumulative flow</h2>
    {{.CFD}}
    <div class="legend">
      <span><span class="dot" style="background:#64748b"></span>Open issues</span>
      <span><span class="dot" style="background:#d97706"></span>Draft PRs</span>
      <span><span class="dot" style="background:#2563eb"></span>Ready PRs</span>
      <span><span class="dot" style="background:#16a34a"></span>Completed (closed issues + merged PRs, cumulative)</span>
    </div>
  </section>

  <section>
    <h2>Bottlenecks</h2>
    <h3>Where completed PR time went</h3>
    {{if .PRs.Cycle.HasData}}
    {{.PRs.ShareBar}}
    <table>
      <tr><th>Station</th><th class="num">Trips</th><th class="num">Mean wait</th><th class="num">Share of cycle time</th></tr>
      {{range .PRs.WaitShares}}
        <tr><td><span class="dot" style="background:{{color .Station}}"></span> {{.Label}}</td>
            <td class="num">{{.Count}}</td>
            <td class="num">{{hours .MeanHours}}</td>
            <td class="num">{{pct .Share}}</td></tr>
      {{end}}
    </table>
    {{else}}<div class="chart-empty">No pull requests merged in the window, so there is no completed time to split.</div>{{end}}
    <div class="note">Trips count the completed PRs that actually waited in each station, per
    recorded draft/ready flips. PRs merged without flip events (automation merges, or caches
    without timeline data) have no known draft-versus-review split, so they are reported as one
    unsplit bucket rather than attributed to a station. The issues line has a single active
    station (open), so its cycle time is entirely queue wait by construction.</div>
    <h3>Queue pressure</h3>
    <table>
      <tr><th>Line</th><th>Station</th><th class="num">Now</th><th class="num">Arrived</th><th class="num">Departed</th><th class="num">Net</th><th>Trend</th><th>Weekly WIP</th></tr>
      {{range .Queue}}
        <tr><td>{{.Line}}</td><td>{{.Label}}</td>
            <td class="num">{{.Count}}</td><td class="num">{{.Arrived}}</td>
            <td class="num">{{.Departed}}</td><td class="num">{{if gt .Net 0}}+{{end}}{{.Net}}</td>
            <td><span class="badge {{.Trend}}">{{.Trend}}</span></td>
            <td>{{.Spark}}</td></tr>
      {{end}}
    </table>
    <div class="note">A filling queue receives items faster than it passes them on; it is where
    the line accumulates work.</div>
  </section>

  <section>
    <h2>On the line now</h2>
    {{if .WIP}}
    <table>
      <tr><th>Item</th><th></th><th>Station</th><th class="num">Waiting</th></tr>
      {{range .WIP}}
        <tr>
          <td>{{if .URL}}<a href="{{.URL}}">{{.ID}}</a>{{else}}{{.ID}}{{end}} {{.Title}}</td>
          <td><span class="badge">{{.Kind}}</span></td>
          <td><span class="dot" style="background:{{.Color}}"></span> {{.Station}}</td>
          <td class="num">{{.Age}}</td>
        </tr>
      {{end}}
    </table>
    {{else}}<div class="chart-empty">Nothing is on the line: every item in the window is finished.</div>{{end}}
    {{if .WIPHidden}}<div class="note">&hellip; and {{.WIPHidden}} more items on the line (the JSON API at <code>/api/process</code> lists them all).</div>{{end}}
  </section>

  <section>
    <h2>Cycle time</h2>
    <div class="two-col">
      <div>
        <h3>{{.Issues.Label}} (created &rarr; closed)</h3>
        {{if .Issues.Cycle.HasData}}
        <div class="cards">
          <div class="card"><div class="value">{{.Issues.Cycle.Median}}</div><div class="label">Median</div></div>
          <div class="card"><div class="value">{{.Issues.Cycle.P90}}</div><div class="label">p90</div></div>
          <div class="card"><div class="value">{{.Issues.Cycle.Mean}}</div><div class="label">Mean</div></div>
          <div class="card"><div class="value">{{.Issues.Cycle.Completions}}</div><div class="label">Completed</div></div>
        </div>
        {{.Issues.Histogram}}
        {{else}}<div class="chart-empty">No issues closed in the window.</div>{{end}}
      </div>
      <div>
        <h3>{{.PRs.Label}} (created &rarr; merged)</h3>
        {{if .PRs.Cycle.HasData}}
        <div class="cards">
          <div class="card"><div class="value">{{.PRs.Cycle.Median}}</div><div class="label">Median</div></div>
          <div class="card"><div class="value">{{.PRs.Cycle.P90}}</div><div class="label">p90</div></div>
          <div class="card"><div class="value">{{.PRs.Cycle.Mean}}</div><div class="label">Mean</div></div>
          <div class="card"><div class="value">{{.PRs.Cycle.Completions}}</div><div class="label">Merged</div></div>
        </div>
        {{.PRs.Histogram}}
        {{else}}<div class="chart-empty">No pull requests merged in the window.</div>{{end}}
      </div>
    </div>
  </section>

  <section>
    <h2>Flow</h2>
    <div class="two-col">
      <div>
        <h3>{{.Issues.Label}}</h3>
        <div class="cards">
          <div class="card"><div class="value">{{.Issues.Littles.AvgWIP}}</div><div class="label">Work in process</div></div>
          <div class="card"><div class="value">{{.Issues.Littles.Throughput}}</div><div class="label">Throughput</div></div>
          <div class="card"><div class="value">{{.Issues.Littles.Implied}}</div><div class="label">Implied cycle time</div></div>
          <div class="card"><div class="value">{{.Issues.Forecast.Items}}</div><div class="label">On the line</div></div>
          <div class="card"><div class="value">{{.Issues.Forecast.P50}}</div><div class="label">Forecast p50</div></div>
          <div class="card"><div class="value">{{.Issues.Forecast.P85}}</div><div class="label">Forecast p85</div></div>
        </div>
        <div class="cards" style="margin-top:10px">
          <div class="card"><div class="value">{{.Issues.Outcomes.Created}}</div><div class="label">Created in window</div></div>
          <div class="card"><div class="value">{{.Issues.Outcomes.Done}}</div><div class="label">Closed</div></div>
          <div class="card"><div class="value">{{.Issues.Outcomes.Active}}</div><div class="label">Still open</div></div>
        </div>
        {{.Issues.Weekly}}
        <div class="legend"><span><span class="dot" style="background:#64748b"></span>Created</span>
        <span><span class="dot" style="background:#16a34a"></span>Completed</span></div>
      </div>
      <div>
        <h3>{{.PRs.Label}}</h3>
        <div class="cards">
          <div class="card"><div class="value">{{.PRs.Littles.AvgWIP}}</div><div class="label">Work in process</div></div>
          <div class="card"><div class="value">{{.PRs.Littles.Throughput}}</div><div class="label">Throughput</div></div>
          <div class="card"><div class="value">{{.PRs.Littles.Implied}}</div><div class="label">Implied cycle time</div></div>
          <div class="card"><div class="value">{{.PRs.Forecast.Items}}</div><div class="label">On the line</div></div>
          <div class="card"><div class="value">{{.PRs.Forecast.P50}}</div><div class="label">Forecast p50</div></div>
          <div class="card"><div class="value">{{.PRs.Forecast.P85}}</div><div class="label">Forecast p85</div></div>
        </div>
        <div class="cards" style="margin-top:10px">
          <div class="card"><div class="value">{{.PRs.Outcomes.Created}}</div><div class="label">Created in window</div></div>
          <div class="card"><div class="value">{{.PRs.Outcomes.Done}}</div><div class="label">Merged</div></div>
          <div class="card"><div class="value">{{.PRs.Outcomes.Discarded}}</div><div class="label">Discarded</div></div>
          <div class="card"><div class="value">{{.PRs.Outcomes.Active}}</div><div class="label">Still open</div></div>
        </div>
        {{.PRs.Weekly}}
        <div class="legend"><span><span class="dot" style="background:#64748b"></span>Created</span>
        <span><span class="dot" style="background:#16a34a"></span>Merged</span></div>
      </div>
    </div>
    <div class="note">Little's law: average work in process divided by throughput is the cycle
    time the queue depth implies. Compare it with the observed median to see whether the queue,
    not the work, is the delay. The forecast resamples the window's weekly completions
    ({{.PRs.Forecast.Sims}} simulations) to project when today's items clear.</div>
  </section>

  <div class="note">Served from the ghx cache. Items whose close or merge time GitHub did not
  record count toward inventory and outcomes but not toward cycle times. PR draft/ready history
  comes from cached timeline events, so repositories cached before that data was collected
  show their open PRs at the station their current state implies.</div>
</div>
</body>
</html>
`
