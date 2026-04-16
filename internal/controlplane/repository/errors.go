package repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

var (
	ErrForbidden                 = errors.New("repository operation forbidden")
	ErrDuplicatePendingExpansion = errors.New("duplicate pending expansion")
	ErrPendingScheduleExists     = errors.New("pending budget schedule exists")
	ErrUniqueConflict            = errors.New("unique constraint conflict")
	ErrMyBudgetChainUnavailable  = errors.New("my budget chain unavailable")
)

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var sqlState interface{ SQLState() string }
	if errors.As(err, &sqlState) && sqlState.SQLState() == "23505" {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "unique failed")
}
