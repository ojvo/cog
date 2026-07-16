package util

type PaginationResult struct {
	Items      interface{} `json:"items"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

func (r PaginationResult) WithItems(items interface{}) map[string]interface{} {
	return map[string]interface{}{
		"items":       items,
		"total":       r.Total,
		"page":        r.Page,
		"page_size":   r.PageSize,
		"total_pages": r.TotalPages,
	}
}

func Paginate(total int, page, pageSize, maxPageSize int) PaginationResult {
	if pageSize <= 0 {
		pageSize = 50
	}
	if maxPageSize <= 0 {
		maxPageSize = 200
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	if page < 1 {
		page = 1
	}

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}

	return PaginationResult{
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}

func PaginateSlice[T any](all []T, page, pageSize, maxPageSize int) (paged []T, result PaginationResult) {
	result = Paginate(len(all), page, pageSize, maxPageSize)

	start := (result.Page - 1) * result.PageSize
	if start >= len(all) {
		return []T{}, result
	}
	end := start + result.PageSize
	if end > len(all) {
		end = len(all)
	}
	paged = make([]T, 0, end-start)
	for i := start; i < end; i++ {
		paged = append(paged, all[i])
	}
	result.Items = paged
	return paged, result
}
