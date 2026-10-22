package httpx

import (
	"fmt"
	"net/http"
	"strconv"
)

const DefaultPageSize = 20
const MaxPageSize = 200

type Pagination struct {
	Page     int
	PageSize int
}

// ReadPagination uses Mesgo's 1-based page/pageSize convention. The size is
// capped at 200; the response always reports the effective size.
func ReadPagination(r *http.Request) (Pagination, error) {
	p := Pagination{Page: 1, PageSize: DefaultPageSize}
	for key, dst := range map[string]*int{"page": &p.Page, "pageSize": &p.PageSize} {
		if raw, exists := r.URL.Query()[key]; exists {
			if len(raw) != 1 {
				return p, fmt.Errorf("%s must be a positive integer", key)
			}
			n, err := strconv.Atoi(raw[0])
			if err != nil || n < 1 {
				return p, fmt.Errorf("%s must be a positive integer", key)
			}
			*dst = n
		}
	}
	p.PageSize = min(p.PageSize, MaxPageSize)
	maxInt := int(^uint(0) >> 1)
	if p.Page-1 > maxInt/p.PageSize {
		return p, fmt.Errorf("page is too large")
	}
	return p, nil
}

func (p Pagination) Offset() int { return (p.Page - 1) * p.PageSize }

type PagedList[T any] struct {
	Data       []T `json:"data"`
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalPages int `json:"totalPages"`
}

// NewPagedList wraps data that has already been sliced (for SQL LIMIT/OFFSET).
func NewPagedList[T any](data []T, total int, p Pagination) PagedList[T] {
	if data == nil {
		data = []T{}
	}
	pages := total / p.PageSize
	if total%p.PageSize != 0 {
		pages++
	}
	return PagedList[T]{Data: data, Page: p.Page, PageSize: p.PageSize, TotalPages: pages}
}

// WritePagedList paginates existing ordered feature lists. The student list
// uses SQL pagination instead; larger collections can adopt that repository
// contract later without changing the public response.
func WritePagedList[T any](w http.ResponseWriter, r *http.Request, all []T, extra map[string]any) {
	p, err := ReadPagination(r)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	start := min(p.Offset(), len(all))
	end := start + min(p.PageSize, len(all)-start)
	page := NewPagedList(all[start:end], len(all), p)
	if extra == nil {
		JSON(w, http.StatusOK, page)
		return
	}
	extra["data"], extra["page"], extra["pageSize"], extra["totalPages"] = page.Data, page.Page, page.PageSize, page.TotalPages
	JSON(w, http.StatusOK, extra)
}
