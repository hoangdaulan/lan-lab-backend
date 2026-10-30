package httpx

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestPagedListPagesAndEmptyData(t *testing.T) {
	for _, tc := range []struct {
		name, query            string
		all                    []int
		want                   []int
		page, size, totalPages int
	}{
		{"second page", "?page=2&pageSize=2", []int{1, 2, 3, 4, 5}, []int{3, 4}, 2, 2, 3},
		{"last page", "?page=3&pageSize=2", []int{1, 2, 3, 4, 5}, []int{5}, 3, 2, 3},
		{"beyond end", "?page=4&pageSize=2", []int{1, 2, 3, 4, 5}, []int{}, 4, 2, 3},
		{"empty", "", nil, []int{}, 1, 20, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WritePagedList(w, httptest.NewRequest("GET", "/"+tc.query, nil), tc.all, map[string]any{"server_time": "kept"})
			var got struct {
				PagedList[int]
				ServerTime string `json:"server_time"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || got.Data == nil || len(got.Data) != len(tc.want) || got.Page != tc.page || got.PageSize != tc.size || got.TotalPages != tc.totalPages || got.ServerTime != "kept" {
				t.Fatalf("status=%d response=%s", w.Code, w.Body.String())
			}
			for i, v := range tc.want {
				if got.Data[i] != v {
					t.Fatalf("data=%v want=%v", got.Data, tc.want)
				}
			}
		})
	}
}

func TestInvalidPaginationAndSizeCap(t *testing.T) {
	for _, query := range []string{"page=0", "page=-1", "page=no", "pageSize=0", "pageSize=", "pageSize=2&pageSize=3", "page=9223372036854775807&pageSize=200"} {
		w := httptest.NewRecorder()
		WritePagedList(w, httptest.NewRequest("GET", "/?"+query, nil), []int{1}, nil)
		if w.Code != 400 {
			t.Errorf("%s: got %d", query, w.Code)
		}
	}
	p, err := ReadPagination(httptest.NewRequest("GET", "/?page=2&pageSize=1000", nil))
	if err != nil || p.PageSize != 200 || p.Offset() != 200 {
		t.Fatalf("pagination=%+v err=%v", p, err)
	}
}
