package handler

import (
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/gin-gonic/gin"
)

type RequestQuotaGate interface {
	BeforeUpstream(*gin.Context, string) error
	AfterUpstream(*gin.Context)
}

func PrepareAuthorizedBudget(
	ctx *gin.Context,
	originalGroup string,
	dispatch func() error,
	gate *budget.Gate,
) error {
	_, err := PrepareAuthorizedGates(ctx, originalGroup, dispatch, gate, nil)
	return err
}

func PrepareAuthorizedGates(
	ctx *gin.Context,
	originalGroup string,
	dispatch func() error,
	gate *budget.Gate,
	requestQuota RequestQuotaGate,
) (func(), error) {
	if err := dispatch(); err != nil {
		return nil, err
	}
	if gate != nil {
		started := time.Now()
		if _, err := gate.Check(ctx); err != nil {
			ctx.Header("X-Budget-Check-Ms", strconv.FormatFloat(float64(time.Since(started).Microseconds())/1000, 'f', 3, 64))
			return nil, err
		}
		ctx.Header("X-Budget-Check-Ms", strconv.FormatFloat(float64(time.Since(started).Microseconds())/1000, 'f', 3, 64))
	}
	if requestQuota == nil {
		return nil, nil
	}
	if err := requestQuota.BeforeUpstream(ctx, originalGroup); err != nil {
		return nil, err
	}
	return func() { requestQuota.AfterUpstream(ctx) }, nil
}
