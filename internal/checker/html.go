package checker

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
)

// NamedReport pairs a Report with the display name of what it was run
// against (an assembly file name, or a NuGet package spec) — RenderHTML's
// own input shape, used both for a single `vmnet check`/`check package`
// result (len==1) and for `vmnet analyze`'s own multi-assembly directory
// scan (len>1, where an aggregate roll-up section is also shown).
type NamedReport struct {
	Name   string
	Report *Report
}

// Candidate is one type-level "good migration target" — internal/migrate
// builds these from a Report's own PerType breakdown (a type ranked by
// how clean ITS OWN methods are, not the whole assembly's average) and
// passes them through to RenderHTML for the "best migration candidates"
// section `vmnet analyze` shows. nil/empty for a plain `vmnet check`
// report, which has no cross-assembly candidate-ranking concept.
type Candidate struct {
	Type            string
	Assembly        string
	MethodsAnalyzed int
	MethodsFlagged  int
}

func (c Candidate) CleanRatio() float64 {
	if c.MethodsAnalyzed == 0 {
		return 0
	}
	return float64(c.MethodsAnalyzed-c.MethodsFlagged) / float64(c.MethodsAnalyzed)
}

// apiCount is one line of a "top missing APIs" ranking: a real call
// target (Finding.Detail) and how many findings across the report(s)
// named it.
type apiCount struct {
	Detail string
	Kind   FindingKind
	Count  int
}

// htmlView is RenderHTML's own template input — everything the template
// needs, pre-computed here rather than in the template itself (Go
// templates can express loops/conditionals, but real aggregation logic
// belongs in Go, not template text).
type htmlView struct {
	Title      string
	Generated  string
	Multi      bool
	Aggregate  *aggregateView
	Reports    []reportView
	Candidates []Candidate
	HasCandid  bool
	FooterNote string
}

type aggregateView struct {
	Assemblies      int
	MethodsAnalyzed int
	MethodsFlagged  int
	CleanPct        string
	StatusClass     string
	ByKind          []kindCount
	TopAPIs         []apiCount
}

type kindCount struct {
	Kind    FindingKind
	Label   string
	Count   int
	Percent float64
}

type reportView struct {
	Name            string
	Status          Status
	StatusClass     string
	StatusLabel     string
	Profile         Profile
	MethodsAnalyzed int
	MethodsFlagged  int
	CleanPct        string
	ByKind          []kindCount
	TopAPIs         []apiCount
	Findings        []Finding
	Open            bool
}

// statusClass/statusLabel map a Status to the CSS class and human label
// the report uses for its color-coded pill — kept as functions (not a
// map literal) so an unrecognized/zero Status still degrades to a
// sensible, visible default instead of an empty pill.
func statusClass(s Status) string {
	switch s {
	case StatusCompatible:
		return "good"
	case StatusPartial:
		return "warn"
	case StatusUnsupported:
		return "bad"
	default:
		return "warn"
	}
}

func statusLabel(s Status) string {
	if s == "" {
		return "unknown"
	}
	return string(s)
}

func KindLabel(k FindingKind) string {
	switch k {
	case KindUnsupportedOpcode:
		return "Unsupported opcode"
	case KindUnsupportedMethod:
		return "Unsupported BCL method"
	case KindReflection:
		return "Reflection"
	case KindAsync:
		return "Async/Task"
	case KindPInvoke:
		return "P/Invoke"
	case KindUnsafePointer:
		return "Unsafe pointer"
	case KindOutOfProfile:
		return "Out of profile"
	default:
		return string(k)
	}
}

func pct(clean, total int) string {
	if total == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(clean)/float64(total))
}

// buildKindCounts/buildTopAPIs are shared by both the per-report and the
// cross-report aggregate view — the same two rollups, over a different
// slice of Findings.
func buildKindCounts(findings []Finding) []kindCount {
	counts := map[FindingKind]int{}
	for _, f := range findings {
		counts[f.Kind]++
	}
	total := len(findings)
	out := make([]kindCount, 0, len(counts))
	for k, c := range counts {
		pct := 0.0
		if total > 0 {
			pct = 100 * float64(c) / float64(total)
		}
		out = append(out, kindCount{Kind: k, Label: KindLabel(k), Count: c, Percent: pct})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

const topAPIsLimit = 15

func buildTopAPIs(findings []Finding) []apiCount {
	type key struct {
		detail string
		kind   FindingKind
	}
	counts := map[key]int{}
	for _, f := range findings {
		if f.Detail == "" {
			continue
		}
		counts[key{f.Detail, f.Kind}]++
	}
	out := make([]apiCount, 0, len(counts))
	for k, c := range counts {
		out = append(out, apiCount{Detail: k.detail, Kind: k.kind, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Detail < out[j].Detail
	})
	if len(out) > topAPIsLimit {
		out = out[:topAPIsLimit]
	}
	return out
}

// RenderHTML builds a single, self-contained HTML page (no external
// fonts/scripts/stylesheets — real system font stacks, inline CSS only)
// summarizing reports: compatible/blocked methods, the top missing
// APIs, and every finding grouped by kind, per assembly. candidates is
// optional (nil for a plain `vmnet check` result) — internal/migrate
// populates it from a multi-assembly Report.PerType breakdown for
// `vmnet analyze`'s own "best migration candidates" section.
func RenderHTML(title string, reports []NamedReport, candidates []Candidate, generatedAt string) (string, error) {
	view := htmlView{
		Title:      title,
		Generated:  generatedAt,
		Multi:      len(reports) > 1,
		Candidates: candidates,
		HasCandid:  len(candidates) > 0,
		FooterNote: "Generated by vmnet — reproduce any single result with `vmnet check`/`vmnet check package`/`vmnet analyze`.",
	}

	var allFindings []Finding
	var totalAnalyzed, totalFlagged int
	for _, nr := range reports {
		r := nr.Report
		if r == nil {
			continue
		}
		allFindings = append(allFindings, r.Findings...)
		totalAnalyzed += r.MethodsAnalyzed
		totalFlagged += r.MethodsFlagged

		view.Reports = append(view.Reports, reportView{
			Name:            nr.Name,
			Status:          r.Status,
			StatusClass:     statusClass(r.Status),
			StatusLabel:     statusLabel(r.Status),
			Profile:         r.Profile,
			MethodsAnalyzed: r.MethodsAnalyzed,
			MethodsFlagged:  r.MethodsFlagged,
			CleanPct:        pct(r.MethodsAnalyzed-r.MethodsFlagged, r.MethodsAnalyzed),
			ByKind:          buildKindCounts(r.Findings),
			TopAPIs:         buildTopAPIs(r.Findings),
			Findings:        r.Findings,
			// A single-assembly report opens its own findings by
			// default (there's nothing else to look at); a multi-
			// assembly analyze run starts every card collapsed so the
			// aggregate summary and candidate list are what's visible
			// first, matching how a real dashboard should surface the
			// roll-up before the detail.
			Open: len(reports) == 1,
		})
	}

	if view.Multi {
		overallStatus := StatusCompatible
		switch {
		case totalAnalyzed == 0 || totalFlagged >= totalAnalyzed:
			overallStatus = StatusUnsupported
		case totalFlagged > 0:
			overallStatus = StatusPartial
		}
		view.Aggregate = &aggregateView{
			Assemblies:      len(reports),
			MethodsAnalyzed: totalAnalyzed,
			MethodsFlagged:  totalFlagged,
			CleanPct:        pct(totalAnalyzed-totalFlagged, totalAnalyzed),
			StatusClass:     statusClass(overallStatus),
			ByKind:          buildKindCounts(allFindings),
			TopAPIs:         buildTopAPIs(allFindings),
		}
	}

	sort.Slice(view.Candidates, func(i, j int) bool {
		return view.Candidates[i].CleanRatio() > view.Candidates[j].CleanRatio()
	})

	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"barWidth":  func(pct float64) string { return fmt.Sprintf("%.1f%%", pct) },
		"ratioPct":  func(c Candidate) string { return fmt.Sprintf("%.1f%%", 100*c.CleanRatio()) },
		"join":      strings.Join,
		"sub":       func(a, b int) int { return a - b },
		"kindLabel": KindLabel,
	}).Parse(htmlTemplate)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, view); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const htmlTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
  :root {
    --bg: #10131a;
    --surface: #171b24;
    --surface-2: #1e2330;
    --border: #2a3040;
    --text: #e8ebf2;
    --text-muted: #8b93a7;
    --accent: #6f8eef;
    --good: #3ecf8e;
    --good-bg: rgba(62, 207, 142, 0.12);
    --warn: #e2ab4c;
    --warn-bg: rgba(226, 171, 76, 0.14);
    --bad: #e5645c;
    --bad-bg: rgba(229, 100, 92, 0.14);
    --mono: ui-monospace, "SF Mono", "Cascadia Code", Consolas, "Roboto Mono", monospace;
    --sans: -apple-system, "Segoe UI", ui-sans-serif, Roboto, Helvetica, Arial, sans-serif;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    background: var(--bg);
    color: var(--text);
    font-family: var(--sans);
    line-height: 1.5;
    -webkit-font-smoothing: antialiased;
  }
  a { color: var(--accent); }
  .wrap { max-width: 1080px; margin: 0 auto; padding: 40px 24px 80px; }
  header.page-header { margin-bottom: 32px; }
  header.page-header h1 {
    font-size: 1.65rem;
    margin: 0 0 6px;
    text-wrap: balance;
    letter-spacing: -0.01em;
  }
  header.page-header p { margin: 0; color: var(--text-muted); font-size: 0.9rem; }
  .stat-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: 12px;
    margin-bottom: 28px;
  }
  .stat-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 16px 18px;
  }
  .stat-card .stat-value {
    display: block;
    font-family: var(--mono);
    font-variant-numeric: tabular-nums;
    font-size: 1.6rem;
    font-weight: 600;
  }
  .stat-card.good .stat-value { color: var(--good); }
  .stat-card.warn .stat-value { color: var(--warn); }
  .stat-card.bad .stat-value { color: var(--bad); }
  .stat-card .stat-label {
    display: block;
    margin-top: 4px;
    color: var(--text-muted);
    font-size: 0.78rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  section { margin-bottom: 32px; }
  h2 {
    font-size: 1.05rem;
    margin: 0 0 14px;
    color: var(--text);
  }
  .bar-list { display: flex; flex-direction: column; gap: 8px; }
  .bar-row { display: grid; grid-template-columns: minmax(160px, 320px) 1fr auto; gap: 12px; align-items: center; }
  .bar-row .bar-name {
    font-family: var(--mono);
    font-size: 0.82rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bar-track { background: var(--surface-2); border-radius: 5px; height: 8px; overflow: hidden; }
  .bar-fill { height: 100%; background: var(--accent); border-radius: 5px; }
  .bar-row .bar-count {
    font-family: var(--mono);
    font-variant-numeric: tabular-nums;
    color: var(--text-muted);
    font-size: 0.8rem;
    text-align: right;
    min-width: 3.5em;
  }
  table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
  th, td { text-align: left; padding: 7px 10px; border-bottom: 1px solid var(--border); }
  th { color: var(--text-muted); font-weight: 500; text-transform: uppercase; font-size: 0.72rem; letter-spacing: 0.03em; }
  td.mono, th.num { font-family: var(--mono); }
  td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
  .table-wrap { overflow-x: auto; border: 1px solid var(--border); border-radius: 10px; }
  .table-wrap table { margin: 0; }
  .table-wrap th:first-child, .table-wrap td:first-child { padding-left: 14px; }
  .pill {
    display: inline-flex;
    align-items: center;
    padding: 2px 10px;
    border-radius: 999px;
    font-size: 0.72rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  .pill.good { background: var(--good-bg); color: var(--good); }
  .pill.warn { background: var(--warn-bg); color: var(--warn); }
  .pill.bad { background: var(--bad-bg); color: var(--bad); }
  details.assembly-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    margin-bottom: 12px;
    overflow: hidden;
  }
  details.assembly-card > summary {
    list-style: none;
    cursor: pointer;
    padding: 14px 18px;
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  details.assembly-card > summary::-webkit-details-marker { display: none; }
  details.assembly-card > summary .assembly-name {
    font-family: var(--mono);
    font-weight: 600;
    font-size: 0.92rem;
  }
  details.assembly-card > summary .assembly-stats {
    color: var(--text-muted);
    font-size: 0.82rem;
    margin-left: auto;
    font-family: var(--mono);
    font-variant-numeric: tabular-nums;
  }
  .assembly-body { padding: 4px 18px 18px; border-top: 1px solid var(--border); }
  .assembly-body h3 {
    font-size: 0.85rem;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    margin: 20px 0 10px;
  }
  .assembly-body h3:first-child { margin-top: 16px; }
  .no-findings { color: var(--text-muted); font-size: 0.88rem; padding: 12px 0; }
  code.suggestion { color: var(--text-muted); font-size: 0.8rem; }
  footer {
    color: var(--text-muted);
    font-size: 0.78rem;
    margin-top: 40px;
    padding-top: 16px;
    border-top: 1px solid var(--border);
  }
  footer code { font-family: var(--mono); }
</style>
</head>
<body>
<div class="wrap">
  <header class="page-header">
    <h1>{{.Title}}</h1>
    <p>Generated {{.Generated}}</p>
  </header>

  {{if .Aggregate}}
  <section>
    <h2>Overview</h2>
    <div class="stat-grid">
      <div class="stat-card {{.Aggregate.StatusClass}}">
        <span class="stat-value">{{.Aggregate.Assemblies}}</span>
        <span class="stat-label">Assemblies</span>
      </div>
      <div class="stat-card">
        <span class="stat-value">{{.Aggregate.MethodsAnalyzed}}</span>
        <span class="stat-label">Methods analyzed</span>
      </div>
      <div class="stat-card good">
        <span class="stat-value">{{sub .Aggregate.MethodsAnalyzed .Aggregate.MethodsFlagged}}</span>
        <span class="stat-label">Runnable today</span>
      </div>
      <div class="stat-card bad">
        <span class="stat-value">{{.Aggregate.MethodsFlagged}}</span>
        <span class="stat-label">Blocked</span>
      </div>
      <div class="stat-card {{.Aggregate.StatusClass}}">
        <span class="stat-value">{{.Aggregate.CleanPct}}</span>
        <span class="stat-label">Clean methods</span>
      </div>
    </div>
  </section>

  {{if .Aggregate.ByKind}}
  <section>
    <h2>Blocked by category</h2>
    <div class="bar-list">
      {{range .Aggregate.ByKind}}
      <div class="bar-row">
        <span class="bar-name">{{.Label}}</span>
        <div class="bar-track"><div class="bar-fill" style="width: {{barWidth .Percent}}"></div></div>
        <span class="bar-count">{{.Count}}</span>
      </div>
      {{end}}
    </div>
  </section>
  {{end}}

  {{if .Aggregate.TopAPIs}}
  <section>
    <h2>Top missing APIs</h2>
    <div class="table-wrap">
      <table>
        <thead><tr><th>API</th><th>Category</th><th class="num">Sites</th></tr></thead>
        <tbody>
          {{range .Aggregate.TopAPIs}}
          <tr><td class="mono">{{.Detail}}</td><td>{{kindLabel .Kind}}</td><td class="num">{{.Count}}</td></tr>
          {{end}}
        </tbody>
      </table>
    </div>
  </section>
  {{end}}
  {{end}}

  {{if .HasCandid}}
  <section>
    <h2>Best migration candidates</h2>
    <p style="color:var(--text-muted); font-size:0.85rem; margin-top:-6px;">Types ranked by their own clean-method ratio (methods with zero findings ÷ methods analyzed on that type alone) — a high overall assembly percentage can still hide individual types that are fully blocked, and vice versa.</p>
    <div class="table-wrap">
      <table>
        <thead><tr><th>Type</th><th>Assembly</th><th class="num">Clean</th><th class="num">Methods</th></tr></thead>
        <tbody>
          {{range .Candidates}}
          <tr>
            <td class="mono">{{.Type}}</td>
            <td class="mono">{{.Assembly}}</td>
            <td class="num">{{ratioPct .}}</td>
            <td class="num">{{sub .MethodsAnalyzed .MethodsFlagged}}/{{.MethodsAnalyzed}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
  </section>
  {{end}}

  <section>
    <h2>{{if .Multi}}Assemblies{{else}}Result{{end}}</h2>
    {{range .Reports}}
    <details class="assembly-card"{{if .Open}} open{{end}}>
      <summary>
        <span class="pill {{.StatusClass}}">{{.StatusLabel}}</span>
        <span class="assembly-name">{{.Name}}</span>
        <span class="assembly-stats">{{.MethodsAnalyzed}} methods, {{.MethodsFlagged}} flagged ({{.CleanPct}} clean)</span>
      </summary>
      <div class="assembly-body">
        {{if .ByKind}}
        <h3>Blocked by category</h3>
        <div class="bar-list">
          {{range .ByKind}}
          <div class="bar-row">
            <span class="bar-name">{{.Label}}</span>
            <div class="bar-track"><div class="bar-fill" style="width: {{barWidth .Percent}}"></div></div>
            <span class="bar-count">{{.Count}}</span>
          </div>
          {{end}}
        </div>
        {{end}}

        {{if .TopAPIs}}
        <h3>Top missing APIs</h3>
        <div class="table-wrap">
          <table>
            <thead><tr><th>API</th><th>Category</th><th class="num">Sites</th></tr></thead>
            <tbody>
              {{range .TopAPIs}}
              <tr><td class="mono">{{.Detail}}</td><td>{{kindLabel .Kind}}</td><td class="num">{{.Count}}</td></tr>
              {{end}}
            </tbody>
          </table>
        </div>
        {{end}}

        {{if .Findings}}
        <h3>Findings ({{len .Findings}})</h3>
        <div class="table-wrap">
          <table>
            <thead><tr><th>Category</th><th>Method</th><th>Detail</th></tr></thead>
            <tbody>
              {{range .Findings}}
              <tr>
                <td>{{kindLabel .Kind}}</td>
                <td class="mono">{{if .Method}}{{.Method}}{{else}}(assembly){{end}}</td>
                <td class="mono">{{.Detail}}{{if .Suggestion}}<br><code class="suggestion">{{.Suggestion}}</code>{{end}}</td>
              </tr>
              {{end}}
            </tbody>
          </table>
        </div>
        {{else}}
        <p class="no-findings">No findings — every analyzed method resolves cleanly.</p>
        {{end}}
      </div>
    </details>
    {{end}}
  </section>

  <footer>{{.FooterNote}}</footer>
</div>
</body>
</html>
`
