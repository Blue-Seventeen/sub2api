package usagestats

import (
	"fmt"
	"strconv"
)

func ParseIPOptionsPagination(offsetRaw, limitRaw string) (int, int, error) {
	offset, limit := 0, 50
	if offsetRaw != "" {
		v, err := strconv.Atoi(offsetRaw)
		if err != nil || v < 0 {
			return 0, 0, fmt.Errorf("Invalid offset")
		}
		offset = v
	}
	if limitRaw != "" {
		v, err := strconv.Atoi(limitRaw)
		if err != nil || v <= 0 {
			return 0, 0, fmt.Errorf("Invalid limit")
		}
		limit = v
	}
	if limit > 100 {
		limit = 100
	}
	return offset, limit, nil
}
