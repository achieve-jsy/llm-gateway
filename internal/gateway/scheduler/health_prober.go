package scheduler

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type HealthProber struct {
	healthStore    *HealthStore
	routeTable     *RouteTable
	interval       time.Duration
	maxConcurrency int
	probeTimeout   time.Duration
	stopCh         chan struct{}
	httpClient     *http.Client
}

func NewHealthProber(hs *HealthStore, rt *RouteTable, interval time.Duration, maxConcurrency int, probeTimeout time.Duration) *HealthProber {
	if maxConcurrency <= 0 {
		maxConcurrency = 50
	}
	return &HealthProber{
		healthStore:    hs,
		routeTable:     rt,
		interval:       interval,
		maxConcurrency: maxConcurrency,
		probeTimeout:   probeTimeout,
		stopCh:         make(chan struct{}),
		httpClient: &http.Client{
			Timeout: probeTimeout * 2,
		},
	}
}

func (hp *HealthProber) NotifyFailure(modelID string) {
	hp.healthStore.RecordFailure(modelID)
}

func (hp *HealthProber) Run() {
	ticker := time.NewTicker(hp.interval)
	defer ticker.Stop()

	for {
		select {
		case <-hp.stopCh:
			return
		case <-ticker.C:
			hp.probeAll()
		}
	}
}

func (hp *HealthProber) Stop() {
	close(hp.stopCh)
}

func (hp *HealthProber) probeAll() {
	models := hp.routeTable.GetAllModels()
	if len(models) == 0 {
		return
	}

	sem := make(chan struct{}, hp.maxConcurrency)
	var wg sync.WaitGroup

	for _, model := range models {
		wg.Add(1)
		sem <- struct{}{}
		go func(m *ModelRoutingEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			hp.probeOne(m)
		}(model)
	}

	wg.Wait()
}

func (hp *HealthProber) probeOne(model *ModelRoutingEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), hp.probeTimeout)
	defer cancel()

	url := model.Endpoint + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		hp.healthStore.RecordProbeFailure(fmt.Sprintf("%d", model.ModelID))
		return
	}

	resp, err := hp.httpClient.Do(req)
	if err != nil {
		hp.healthStore.RecordProbeFailure(fmt.Sprintf("%d", model.ModelID))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 || resp.StatusCode == 429 {
		hp.healthStore.RecordProbeSuccess(fmt.Sprintf("%d", model.ModelID))
	} else {
		hp.healthStore.RecordProbeFailure(fmt.Sprintf("%d", model.ModelID))
	}
}
