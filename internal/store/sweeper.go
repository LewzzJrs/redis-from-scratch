package store

import (
	"sync"
	"time"
)

type SweeperConfig struct {
	Interval         time.Duration
	SampleSize       int
	RepeatAboveRatio float64
	MaxCycleDuration time.Duration
}

func DefaultSweeperConfig() SweeperConfig {
	return SweeperConfig{
		Interval:         100 * time.Millisecond,
		SampleSize:       20,
		RepeatAboveRatio: 0.25,
		MaxCycleDuration: 25 * time.Millisecond,
	}
}

type Sweeper struct {
	store    *Store
	config   SweeperConfig
	stop     chan struct{}
	stopOnce sync.Once
	finished chan struct{}
}

func StartSweeper(dataStore *Store, config SweeperConfig) *Sweeper {
	sweeper := &Sweeper{
		store:    dataStore,
		config:   config,
		stop:     make(chan struct{}),
		finished: make(chan struct{}),
	}
	go sweeper.run()
	return sweeper
}

func (s *Sweeper) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
	})
	<-s.finished
}

func (s *Sweeper) RunCycle() int {
	cycleStartedAt := time.Now()
	totalDeleted := 0

	for {
		sampledCount, deletedCount := s.store.DeleteExpiredSample(s.config.SampleSize)
		totalDeleted += deletedCount

		expiredRatio := 0.0
		if sampledCount > 0 {
			expiredRatio = float64(deletedCount) / float64(sampledCount)
		}

		mostSampledKeysWereFresh := expiredRatio <= s.config.RepeatAboveRatio
		timeBudgetExhausted := time.Since(cycleStartedAt) >= s.config.MaxCycleDuration
		if mostSampledKeysWereFresh || timeBudgetExhausted {
			return totalDeleted
		}
	}
}

func (s *Sweeper) run() {
	defer close(s.finished)

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.RunCycle()
		}
	}
}
