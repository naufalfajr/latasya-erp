package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePage_Defaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/list", nil)
	p := ParsePage(r)
	if p.PageNum != 1 {
		t.Errorf("expected PageNum=1, got %d", p.PageNum)
	}
	if p.PerPage != 50 {
		t.Errorf("expected PerPage=50, got %d", p.PerPage)
	}
}

func TestParsePage_Clamping(t *testing.T) {
	tests := []struct {
		name     string
		page     string
		perPage  string
		wantPage int
		wantPP   int
	}{
		{"page=0", "0", "", 1, 50},
		{"page=-5", "-5", "", 1, 50},
		{"per_page=0", "", "0", 1, 50},
		{"per_page=-1", "", "-1", 1, 50},
		{"per_page=999", "", "999", 1, 200},
		{"per_page=200", "", "200", 1, 200},
		{"per_page=1", "", "1", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/list"
			if tt.page != "" {
				url += "?page=" + tt.page
			}
			if tt.perPage != "" {
				if tt.page != "" {
					url += "&per_page=" + tt.perPage
				} else {
					url += "?per_page=" + tt.perPage
				}
			}
			r := httptest.NewRequest("GET", url, nil)
			p := ParsePage(r)
			if p.PageNum != tt.wantPage {
				t.Errorf("PageNum: expected %d, got %d", tt.wantPage, p.PageNum)
			}
			if p.PerPage != tt.wantPP {
				t.Errorf("PerPage: expected %d, got %d", tt.wantPP, p.PerPage)
			}
		})
	}
}

func TestParsePage_Valid(t *testing.T) {
	r := httptest.NewRequest("GET", "/list?page=3&per_page=25", nil)
	p := ParsePage(r)
	if p.PageNum != 3 {
		t.Errorf("expected PageNum=3, got %d", p.PageNum)
	}
	if p.PerPage != 25 {
		t.Errorf("expected PerPage=25, got %d", p.PerPage)
	}
}

func TestBuildMeta(t *testing.T) {
	tests := []struct {
		name           string
		page           Page
		total          int
		wantPage       int
		wantPerPage    int
		wantTotal      int
		wantTotalPages int
	}{
		{"total=0", Page{1, 50}, 0, 1, 50, 0, 0},
		{"total=1,pp=50", Page{1, 50}, 1, 1, 50, 1, 1},
		{"total=50,pp=50", Page{1, 50}, 50, 1, 50, 50, 1},
		{"total=51,pp=50", Page{1, 50}, 51, 1, 50, 51, 2},
		{"total=100,pp=50", Page{1, 50}, 100, 1, 50, 100, 2},
		{"total=101,pp=50", Page{1, 50}, 101, 1, 50, 101, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := BuildMeta(tt.page, tt.total)
			if m.Page != tt.wantPage {
				t.Errorf("Page: expected %d, got %d", tt.wantPage, m.Page)
			}
			if m.PerPage != tt.wantPerPage {
				t.Errorf("PerPage: expected %d, got %d", tt.wantPerPage, m.PerPage)
			}
			if m.Total != tt.wantTotal {
				t.Errorf("Total: expected %d, got %d", tt.wantTotal, m.Total)
			}
			if m.TotalPages != tt.wantTotalPages {
				t.Errorf("TotalPages: expected %d, got %d", tt.wantTotalPages, m.TotalPages)
			}
		})
	}
}

func TestPage_Offset(t *testing.T) {
	tests := []struct {
		page   Page
		offset int
	}{
		{Page{1, 50}, 0},
		{Page{2, 50}, 50},
		{Page{3, 25}, 50},
	}
	for _, tt := range tests {
		if got := tt.page.Offset(); got != tt.offset {
			t.Errorf("Page{%d,%d}.Offset(): expected %d, got %d", tt.page.PageNum, tt.page.PerPage, tt.offset, got)
		}
	}
}

func TestWriteList(t *testing.T) {
	w := httptest.NewRecorder()
	data := []string{"a", "b"}
	page := Page{PageNum: 1, PerPage: 50}
	total := 2

	WriteList(w, http.StatusOK, data, page, total)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var env ListEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if env.Meta.Total != 2 {
		t.Errorf("expected meta.total=2, got %d", env.Meta.Total)
	}
	if env.Meta.TotalPages != 1 {
		t.Errorf("expected meta.total_pages=1, got %d", env.Meta.TotalPages)
	}

	if env.Data == nil {
		t.Error("expected data to be present")
	}
}
