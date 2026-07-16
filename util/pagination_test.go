package util

import "testing"

func TestPaginate(t *testing.T) {
	r := Paginate(100, 2, 10, 200)
	if r.Total != 100 {
		t.Errorf("Total = %d, want 100", r.Total)
	}
	if r.Page != 2 {
		t.Errorf("Page = %d, want 2", r.Page)
	}
	if r.PageSize != 10 {
		t.Errorf("PageSize = %d, want 10", r.PageSize)
	}
	if r.TotalPages != 10 {
		t.Errorf("TotalPages = %d, want 10", r.TotalPages)
	}
}

func TestPaginateDefaults(t *testing.T) {
	r := Paginate(100, 0, 0, 0)
	if r.Page != 1 {
		t.Errorf("Page = %d, want 1", r.Page)
	}
	if r.PageSize != 50 {
		t.Errorf("PageSize = %d, want 50 (default)", r.PageSize)
	}
}

func TestPaginateMaxPageSize(t *testing.T) {
	r := Paginate(100, 1, 500, 200)
	if r.PageSize != 200 {
		t.Errorf("PageSize = %d, want 200 (capped)", r.PageSize)
	}
}

func TestPaginateZeroTotal(t *testing.T) {
	r := Paginate(0, 1, 10, 200)
	if r.TotalPages != 1 {
		t.Errorf("TotalPages = %d, want 1 (minimum)", r.TotalPages)
	}
}

func TestPaginateRounding(t *testing.T) {
	r := Paginate(11, 1, 10, 200)
	if r.TotalPages != 2 {
		t.Errorf("TotalPages = %d, want 2", r.TotalPages)
	}
}

func TestPaginateSlice(t *testing.T) {
	all := []int{1, 2, 3, 4, 5}
	paged, r := PaginateSlice(all, 1, 2, 200)
	if len(paged) != 2 {
		t.Fatalf("len(paged) = %d, want 2", len(paged))
	}
	if paged[0] != 1 || paged[1] != 2 {
		t.Errorf("paged = %v, want [1 2]", paged)
	}
	if r.Total != 5 {
		t.Errorf("Total = %d, want 5", r.Total)
	}
}

func TestPaginateSliceOutOfRange(t *testing.T) {
	all := []int{1, 2, 3}
	paged, _ := PaginateSlice(all, 10, 10, 200)
	if len(paged) != 0 {
		t.Errorf("out of range page should return empty, got %d items", len(paged))
	}
}

func TestPaginateSliceLastPage(t *testing.T) {
	all := []int{1, 2, 3, 4, 5}
	paged, _ := PaginateSlice(all, 2, 3, 200)
	if len(paged) != 2 {
		t.Fatalf("len(paged) = %d, want 2", len(paged))
	}
	if paged[0] != 4 || paged[1] != 5 {
		t.Errorf("paged = %v, want [4 5]", paged)
	}
}

func TestPaginationResultWithItems(t *testing.T) {
	r := PaginationResult{Total: 10, Page: 1, PageSize: 5, TotalPages: 2}
	m := r.WithItems([]string{"a", "b"})
	if m["items"] == nil {
		t.Error("WithItems should set items")
	}
	if m["total"] != 10 {
		t.Errorf("total = %v, want 10", m["total"])
	}
}
