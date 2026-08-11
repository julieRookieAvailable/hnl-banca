package transactions

import (
	"net/http/httptest"
	"testing"
)

func TestPaginate(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		def     int
		max     int
		wantLim int
		wantOff int
	}{
		{name: "defaults", query: "", def: 100, max: 200, wantLim: 100, wantOff: 0},
		{name: "valid", query: "limit=10&offset=20", def: 100, max: 200, wantLim: 10, wantOff: 20},
		{name: "limit clamped", query: "limit=500&offset=5", def: 100, max: 200, wantLim: 200, wantOff: 5},
		{name: "limit zero falls back", query: "limit=0&offset=3", def: 100, max: 200, wantLim: 100, wantOff: 3},
		{name: "limit garbage", query: "limit=abc&offset=2", def: 100, max: 200, wantLim: 100, wantOff: 2},
		{name: "offset negative clamped", query: "limit=5&offset=-1", def: 100, max: 200, wantLim: 5, wantOff: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/x?"+tc.query, nil)
			limit, offset := paginate(r, tc.def, tc.max)
			if limit != tc.wantLim || offset != tc.wantOff {
				t.Fatalf("paginate(%q) = (%d, %d), esperaba (%d, %d)", tc.query, limit, offset, tc.wantLim, tc.wantOff)
			}
		})
	}
}
