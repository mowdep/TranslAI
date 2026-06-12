package server

import (
	"log/slog"
	"sync"
	"time"
)

const (
	// FlushDebounce est le delai apres le dernier edit avant flush d'un job.
	FlushDebounce = 3 * time.Second
	// FlushInterval est la periode du ticker plafond pour forcer FlushAll.
	FlushInterval = 30 * time.Second
)

// FlushManager orchestre le write-behind : debounce par job + ticker plafond.
type FlushManager struct {
	store   ReviewStore
	workDir string

	mu       sync.Mutex
	timers   map[string]*time.Timer // debounce par job
	ticker   *time.Ticker
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewFlushManager cree un FlushManager. Appeler Start() pour demarrer.
func NewFlushManager(store ReviewStore, workDir string) *FlushManager {
	return &FlushManager{
		store:   store,
		workDir: workDir,
		timers:  make(map[string]*time.Timer),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Start demarre le ticker plafond en arriere-plan.
func (fm *FlushManager) Start() {
	fm.ticker = time.NewTicker(FlushInterval)
	go fm.run()
}

// run est la boucle principale du FlushManager.
func (fm *FlushManager) run() {
	defer close(fm.doneCh)
	for {
		select {
		case <-fm.stopCh:
			return
		case <-fm.ticker.C:
			if err := fm.store.FlushAll(fm.workDir); err != nil {
				slog.Error("flush_manager: FlushAll ticker", "err", err)
			}
		}
	}
}

// NotifyEdit declenche un debounce pour jobID.
// Si un timer est deja en cours pour ce job, il est remis a zero.
func (fm *FlushManager) NotifyEdit(jobID string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if t, ok := fm.timers[jobID]; ok {
		t.Reset(FlushDebounce)
		return
	}
	fm.timers[jobID] = time.AfterFunc(FlushDebounce, func() {
		if err := fm.store.Flush(jobID, fm.workDir); err != nil {
			slog.Error("flush_manager: debounce flush", "job", jobID, "err", err)
		}
		fm.mu.Lock()
		delete(fm.timers, jobID)
		fm.mu.Unlock()
	})
}

// FlushAllSync effectue un flush synchrone de tous les jobs dirty.
// A appeler avant http.Server.Shutdown (SIGTERM).
func (fm *FlushManager) FlushAllSync() {
	// Annuler les timers debounce en cours pour eviter les double-flush.
	fm.mu.Lock()
	for id, t := range fm.timers {
		t.Stop()
		delete(fm.timers, id)
	}
	fm.mu.Unlock()

	if err := fm.store.FlushAll(fm.workDir); err != nil {
		slog.Error("flush_manager: SIGTERM FlushAll", "err", err)
	}
}

// Stop arrete le ticker plafond et attend la fin de la goroutine.
func (fm *FlushManager) Stop() {
	if fm.ticker != nil {
		fm.ticker.Stop()
	}
	close(fm.stopCh)
	<-fm.doneCh
}
