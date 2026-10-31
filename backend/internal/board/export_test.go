package board

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

func strp(s string) *string { return &s }

func TestWriteCSV(t *testing.T) {
	sum := Summary{ProblemsTotal: 3, Problems: []Problem{{ID: 1, Code: "A"}, {ID: 2, Code: "B"}, {ID: 3, Code: "C"}},
		Rows: []Row{
			{StudentCode: "B21DCCN001", Fullname: "Nguyễn Văn Ánh", Status: "finished", ACCount: 1, Cells: []Cell{
				{State: CellAC},
				{State: CellAttempted, Latest: &Latest{Status: strp("completed"), Verdict: strp("TLE"), PassedTests: 10, TotalTests: 15}},
				{State: CellNone},
			}},
			{StudentCode: "SV2", Fullname: "=HYPERLINK(\"http://x\")", Status: "in_progress", Cells: []Cell{
				{State: CellPending}, {State: CellAttempted, Latest: &Latest{Status: strp("system_error")}}, {State: CellNone},
			}},
		}}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, sum); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\xef\xbb\xbf") {
		t.Error("missing UTF-8 BOM")
	}
	if !strings.Contains(out, "\r\n") {
		t.Error("lines are not CRLF")
	}
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(out, "\xef\xbb\xbf"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"student_code", "fullname", "status", "A", "B", "C", "ac"},
		{"B21DCCN001", "Nguyễn Văn Ánh", "finished", "AC", "10/15 TLE", "", "1/3 AC"},
		{"SV2", "'=HYPERLINK(\"http://x\")", "in_progress", "pending", "system_error", "", "0/3 AC"},
	}
	if len(recs) != len(want) {
		t.Fatalf("records = %v", recs)
	}
	for i := range want {
		if strings.Join(recs[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d = %q, want %q", i, recs[i], want[i])
		}
	}
}
