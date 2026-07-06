package checker

import (
	"strings"
	"testing"
)

// TestRenderHTML_SingleReport proves a single-assembly report renders a
// well-formed, self-contained HTML page: real HTML escaping for a C#
// generic name (which contains angle brackets and a backtick — raw in
// the DOM these would corrupt the page, not just look ugly) and every
// stat that's supposed to be visible actually shows up in the output.
func TestRenderHTML_SingleReport(t *testing.T) {
	report := &Report{
		AssemblyName:    "Vmnet.Fixtures",
		Profile:         ProfileNetStandardLite,
		MethodsAnalyzed: 10,
		MethodsFlagged:  2,
		Findings: []Finding{
			{Kind: KindUnsupportedMethod, Method: "Foo::Bar", Detail: "System.Collections.Generic.List`1<System.String>::Sort", Suggestion: "this BCL method has no native implementation yet"},
			{Kind: KindReflection, Method: "Foo::Baz", Detail: "System.Reflection.Emit.DynamicMethod::CreateDelegate"},
		},
	}
	report.finalize()

	html, err := RenderHTML("Test report", []NamedReport{{Name: "Vmnet.Fixtures.dll", Report: report}}, nil, "2026-01-01")
	if err != nil {
		t.Fatalf("RenderHTML() error = %v", err)
	}

	if !strings.HasPrefix(html, "<!doctype html>") {
		t.Error("RenderHTML() output doesn't start with a doctype")
	}
	if !strings.Contains(html, "Vmnet.Fixtures.dll") {
		t.Error("RenderHTML() output missing the assembly name")
	}
	if !strings.Contains(html, "10 methods, 2 flagged") {
		t.Error("RenderHTML() output missing the methods-analyzed/flagged summary")
	}
	// The generic type name must be HTML-escaped (the literal `<`/`>`
	// would otherwise be parsed as real tags, corrupting the page) —
	// html/template's own auto-escaping should produce &#39;-style
	// entities or &lt;/&gt;, never a raw `<System.String>`.
	if strings.Contains(html, "<System.String>") {
		t.Error("RenderHTML() output contains an unescaped `<System.String>` — a real HTML injection/corruption risk")
	}
	if !strings.Contains(html, "&#34;") && !strings.Contains(html, "&lt;System.String&gt;") {
		// Either escaping form is fine; just confirm SOME escaping of
		// the dangerous characters happened.
		if strings.Contains(html, "List`1<System.String>") {
			t.Error("RenderHTML() output has the raw, unescaped generic name")
		}
	}
	if !strings.Contains(html, "partial") {
		t.Error("RenderHTML() output missing the expected partial status")
	}
}

// TestRenderHTML_MultiReportAggregate proves the aggregate roll-up
// section only appears for more than one report, and correctly sums
// across all of them.
func TestRenderHTML_MultiReportAggregate(t *testing.T) {
	r1 := &Report{MethodsAnalyzed: 100, MethodsFlagged: 10, Findings: []Finding{{Kind: KindReflection, Detail: "System.Reflection.Assembly::GetType"}}}
	r1.finalize()
	r2 := &Report{MethodsAnalyzed: 50, MethodsFlagged: 0}
	r2.finalize()

	html, err := RenderHTML("Multi", []NamedReport{{Name: "A.dll", Report: r1}, {Name: "B.dll", Report: r2}}, nil, "2026-01-01")
	if err != nil {
		t.Fatalf("RenderHTML() error = %v", err)
	}
	if !strings.Contains(html, "Overview") {
		t.Error("RenderHTML() with 2 reports should show the aggregate Overview section")
	}
	// 150 total methods analyzed, 10 flagged.
	if !strings.Contains(html, ">150<") {
		t.Errorf("RenderHTML() aggregate methods-analyzed total wrong or missing; want 150 somewhere")
	}
}

// TestRenderHTML_SingleReportNoAggregate proves a single report does
// NOT show the aggregate section (nothing to aggregate).
func TestRenderHTML_SingleReportNoAggregate(t *testing.T) {
	r := &Report{MethodsAnalyzed: 5, MethodsFlagged: 0}
	r.finalize()
	html, err := RenderHTML("Solo", []NamedReport{{Name: "A.dll", Report: r}}, nil, "2026-01-01")
	if err != nil {
		t.Fatalf("RenderHTML() error = %v", err)
	}
	if strings.Contains(html, "Overview") {
		t.Error("RenderHTML() with 1 report should not show the aggregate Overview section")
	}
}

// TestRenderHTML_Candidates proves the "best migration candidates"
// section renders and ranks by clean ratio, highest first.
func TestRenderHTML_Candidates(t *testing.T) {
	r := &Report{MethodsAnalyzed: 10, MethodsFlagged: 0}
	r.finalize()
	candidates := []Candidate{
		{Type: "Billing.Rules.TaxCalculator", Assembly: "Billing.dll", MethodsAnalyzed: 20, MethodsFlagged: 2}, // 90%
		{Type: "Pricing.Discounts.Engine", Assembly: "Pricing.dll", MethodsAnalyzed: 10, MethodsFlagged: 0},    // 100%
		{Type: "Validation.CustomerValidator", Assembly: "Val.dll", MethodsAnalyzed: 8, MethodsFlagged: 4},     // 50%
	}
	html, err := RenderHTML("Candidates", []NamedReport{{Name: "A.dll", Report: r}}, candidates, "2026-01-01")
	if err != nil {
		t.Fatalf("RenderHTML() error = %v", err)
	}
	if !strings.Contains(html, "Best migration candidates") {
		t.Fatal("RenderHTML() with candidates should show the candidates section")
	}
	pricingIdx := strings.Index(html, "Pricing.Discounts.Engine")
	billingIdx := strings.Index(html, "Billing.Rules.TaxCalculator")
	valIdx := strings.Index(html, "Validation.CustomerValidator")
	if pricingIdx == -1 || billingIdx == -1 || valIdx == -1 {
		t.Fatal("RenderHTML() missing one or more candidate rows")
	}
	if !(pricingIdx < billingIdx && billingIdx < valIdx) {
		t.Errorf("RenderHTML() candidates not sorted best-first: pricing=%d billing=%d val=%d", pricingIdx, billingIdx, valIdx)
	}
}
