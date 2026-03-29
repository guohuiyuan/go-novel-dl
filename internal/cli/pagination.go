package cli

const (
	defaultCLISearchPageSize = 30
	defaultSearchResultLimit = defaultCLISearchPageSize * 5
)

func resultPageCount(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	pages := total / pageSize
	if total%pageSize != 0 {
		pages++
	}
	return pages
}

func resultPageBounds(total, page, pageSize int) (int, int) {
	if total <= 0 || pageSize <= 0 {
		return 0, 0
	}
	totalPages := resultPageCount(total, pageSize)
	if totalPages <= 0 {
		return 0, 0
	}
	page = clampInt(page, 0, totalPages-1)
	start := page * pageSize
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}

func resultPageForIndex(index, pageSize int) int {
	if index <= 0 || pageSize <= 0 {
		return 0
	}
	return index / pageSize
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
