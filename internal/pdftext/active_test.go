package pdftext

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

// activePDF is a one-page document whose catalog carries extra entries and
// whose page carries extra annotations.
func activePDF(catalog, annots string, extra ...string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R " + catalog + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Contents 4 0 R /Annots [" + annots + "] >>",
		pdftest.Stream("", "BT ET"),
	}
	return pdftest.Build(append(objects, extra...)...)
}

func TestInspectReportsActiveContentAndCountsLinks(t *testing.T) {
	for name, tc := range map[string]struct {
		pdf   []byte
		want  []string
		links int
	}{
		"plain":                {activePDF("", ""), nil, 0},
		"open at a page":       {activePDF("/OpenAction [3 0 R /Fit]", ""), nil, 0},
		"go-to on open":        {activePDF("/OpenAction << /S /GoTo /D [3 0 R /Fit] >>", ""), nil, 0},
		"script on open":       {activePDF("/OpenAction << /S /JavaScript /JS (app.alert(1)) >>", ""), []string{activeAutomatic, activeJavaScript}, 0},
		"named scripts":        {activePDF("/Names << /JavaScript 5 0 R >>", "", "<< /Names [(a) 6 0 R] >>", "<< /S /JavaScript /JS (x) >>"), []string{activeJavaScript}, 0},
		"launch from a link":   {activePDF("", "5 0 R", "<< /Type /Annot /Subtype /Link /A << /S /Launch /F (calc.exe) >> >>"), []string{activeLaunch}, 0},
		"attached file":        {activePDF("/Names << /EmbeddedFiles << /Names [(a.exe) 5 0 R] >> >>", "", "<< /Type /Filespec /F (a.exe) /EF << /F 6 0 R >> >>", pdftest.Stream("/Type /EmbeddedFile", "MZ")), []string{activeEmbedded}, 0},
		"attachment note":      {activePDF("", "5 0 R", "<< /Type /Annot /Subtype /FileAttachment >>"), []string{activeEmbedded}, 0},
		"form submission":      {activePDF("", "5 0 R", "<< /Type /Annot /Subtype /Widget /A << /S /SubmitForm /F (https://x) >> >>"), []string{activeSubmit}, 0},
		"remote document":      {activePDF("", "5 0 R", "<< /Type /Annot /Subtype /Link /A << /S /GoToR /F (other.pdf) >> >>"), []string{activeRemote}, 0},
		"xfa form":             {activePDF("/AcroForm << /Fields [] /XFA 5 0 R >>", "", pdftest.Stream("", "<xdp/>")), []string{activeXFA}, 0},
		"web links":            {activePDF("", "5 0 R 6 0 R", "<< /Type /Annot /Subtype /Link /A << /S /URI /URI (https://doi.org/x) >> >>", "<< /Type /Annot /Subtype /Link /A << /S /URI /URI (https://e.org) >> >>"), nil, 2},
		"automatic page event": {activePDF("", "5 0 R", "<< /Type /Annot /Subtype /Widget /AA << /O << /S /GoTo /D [3 0 R /Fit] >> >> >>"), []string{activeAutomatic}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			facts, err := Inspect(context.Background(), tc.pdf, Defaults())
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want
			if want == nil {
				want = []string{}
			}
			if !slices.Equal(facts.ActiveContent, want) || facts.WebLinks != tc.links || !facts.ActiveExamined {
				t.Fatalf("active %v links %d examined %t, want %v %d", facts.ActiveContent, facts.WebLinks, facts.ActiveExamined, want, tc.links)
			}
		})
	}
}

// A graph that cycles through itself is examined once per object, and one
// too large for the budget is reported as not fully examined.
func TestActiveWalkStopsAtCyclesAndItsBudget(t *testing.T) {
	cyclic := activePDF("/Loop 5 0 R", "", "<< /Next 6 0 R >>", "<< /Next 5 0 R /Back 5 0 R >>")
	facts, err := Inspect(context.Background(), cyclic, Defaults())
	if err != nil || !facts.ActiveExamined || len(facts.ActiveContent) != 0 {
		t.Fatal("a cycle was not handled", facts, err)
	}
	// A long chain of distinct objects exceeds the nesting bound.
	var chain []string
	for i := 0; i < activeDepth+5; i++ {
		chain = append(chain, "<< /Next "+strconv.Itoa(6+i)+" 0 R >>")
	}
	deep := activePDF("/Chain 5 0 R", "", chain...)
	facts, err = Inspect(context.Background(), deep, Defaults())
	if err != nil || facts.ActiveExamined || !slices.Contains(facts.ActiveContent, activeOverBudget) {
		t.Fatal("an unexamined graph passed", facts.ActiveContent, facts.ActiveExamined, err)
	}
}
