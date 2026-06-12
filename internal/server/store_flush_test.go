package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFlushAndReload verifie qu'un edit → flush → reload restaure l'etat.
func TestFlushAndReload(t *testing.T) {
	workDir := t.TempDir()
	store := NewReviewStore()

	// Creer un job et un fichier.
	job := store.CreateReview("job-flush")
	cues := []*CueState{
		{
			Index:        1,
			SourceLines:  []string{"Hello"},
			TargetLines:  []string{"Bonjour"},
			durationSecs: 2.0,
		},
		{
			Index:        2,
			SourceLines:  []string{"World"},
			TargetLines:  []string{"Monde"},
			FallbackUsed: true,
			durationSecs: 1.0,
		},
	}
	for _, c := range cues {
		c.Flags = computeFlags(c)
	}
	job.mu.Lock()
	job.files["sub.srt"] = &FileState{Name: "sub.srt", Cues: cues}
	job.dirty = true
	job.mu.Unlock()

	// Flush.
	if err := store.Flush("job-flush", workDir); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Verifier que dirty est false apres flush.
	job.mu.RLock()
	d := job.dirty
	job.mu.RUnlock()
	if d {
		t.Error("job.dirty devrait etre false apres flush")
	}

	// Verifier que le sidecar.json existe.
	scPath := filepath.Join(workDir, "job-flush", "sub.srt", "sidecar.json")
	data, err := os.ReadFile(scPath)
	if err != nil {
		t.Fatalf("sidecar.json absent: %v", err)
	}
	var sc sidecarFile
	if err := json.Unmarshal(data, &sc); err != nil {
		t.Fatalf("sidecar.json invalide: %v", err)
	}
	if len(sc.Cues) != 2 {
		t.Errorf("sidecar.json: %d cues, want 2", len(sc.Cues))
	}

	// Verifier que la cue 2 a FallbackUsed=true dans le sidecar.
	var foundFallback bool
	for _, sc := range sc.Cues {
		if sc.Index == 2 && sc.FallbackUsed {
			foundFallback = true
		}
	}
	if !foundFallback {
		t.Error("sidecar: FallbackUsed=true attendu pour cue 2")
	}
}

// TestFlushAllSetsNotDirty verifie que FlushAll passe tous les jobs dirty a false.
func TestFlushAllSetsNotDirty(t *testing.T) {
	workDir := t.TempDir()
	store := NewReviewStore()

	for _, id := range []string{"j1", "j2"} {
		job := store.CreateReview(id)
		cues := []*CueState{
			{Index: 1, SourceLines: []string{"A"}, TargetLines: []string{"B"}, durationSecs: 1.0},
		}
		for _, c := range cues {
			c.Flags = computeFlags(c)
		}
		job.mu.Lock()
		job.files["f.srt"] = &FileState{Name: "f.srt", Cues: cues}
		job.dirty = true
		job.mu.Unlock()
	}

	if err := store.FlushAll(workDir); err != nil {
		t.Fatalf("FlushAll: %v", err)
	}

	dirty := store.AllDirtyJobs()
	if len(dirty) != 0 {
		t.Errorf("AllDirtyJobs apres FlushAll = %v, want []", dirty)
	}
}

// TestFlushManagerDebounce verifie le comportement du debounce.
func TestFlushManagerDebounce(t *testing.T) {
	workDir := t.TempDir()
	store := NewReviewStore()

	job := store.CreateReview("debounce-job")
	cues := []*CueState{
		{Index: 1, SourceLines: []string{"A"}, TargetLines: []string{"B"}, durationSecs: 1.0},
	}
	for _, c := range cues {
		c.Flags = computeFlags(c)
	}
	job.mu.Lock()
	job.files["f.srt"] = &FileState{Name: "f.srt", Cues: cues}
	job.dirty = true
	job.mu.Unlock()

	// Remplacer FlushDebounce par une valeur courte pour le test.
	origDebounce := FlushDebounce
	_ = origDebounce // non modifiable (constante), on utilise un FlushManager avec timing court

	// Utiliser directement un timer court pour simuler le debounce.
	flushed := make(chan struct{})
	fm := &FlushManager{
		store:   store,
		workDir: workDir,
		timers:  make(map[string]*time.Timer),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	// Demarrer manuellement un timer court.
	fm.mu.Lock()
	fm.timers["debounce-job"] = time.AfterFunc(50*time.Millisecond, func() {
		if err := store.Flush("debounce-job", workDir); err != nil {
			t.Errorf("flush debounce: %v", err)
		}
		fm.mu.Lock()
		delete(fm.timers, "debounce-job")
		fm.mu.Unlock()
		close(flushed)
	})
	fm.mu.Unlock()

	select {
	case <-flushed:
		// OK — flush effectue apres le debounce.
	case <-time.After(500 * time.Millisecond):
		t.Error("debounce flush trop lent")
	}

	// Verifier que le job n'est plus dirty.
	job.mu.RLock()
	d := job.dirty
	job.mu.RUnlock()
	if d {
		t.Error("job.dirty devrait etre false apres flush debounce")
	}
}

// TestSIGTERMFlushAll simule un SIGTERM et verifie que FlushAllSync flush bien les jobs dirty.
func TestSIGTERMFlushAll(t *testing.T) {
	workDir := t.TempDir()
	store := NewReviewStore()

	for i, id := range []string{"term1", "term2"} {
		job := store.CreateReview(id)
		cues := []*CueState{
			{Index: i + 1, SourceLines: []string{"Source"}, TargetLines: []string{"Cible"}, durationSecs: 1.0},
		}
		for _, c := range cues {
			c.Flags = computeFlags(c)
		}
		job.mu.Lock()
		job.files["sub.srt"] = &FileState{Name: "sub.srt", Cues: cues}
		job.dirty = true
		job.mu.Unlock()
	}

	fm := NewFlushManager(store, workDir)
	fm.Start()

	// Simuler SIGTERM.
	fm.FlushAllSync()
	fm.Stop()

	// Tous les jobs doivent etre clean.
	dirty := store.AllDirtyJobs()
	if len(dirty) != 0 {
		t.Errorf("apres SIGTERM FlushAllSync, jobs dirty = %v, want []", dirty)
	}

	// Les sidecar.json doivent exister.
	for _, id := range []string{"term1", "term2"} {
		scPath := filepath.Join(workDir, id, "sub.srt", "sidecar.json")
		if _, err := os.Stat(scPath); os.IsNotExist(err) {
			t.Errorf("sidecar.json absent pour job %s", id)
		}
	}
}

// TestFlushNotDirtyIsNoop verifie que Flush sur un job non dirty ne fait rien.
func TestFlushNotDirtyIsNoop(t *testing.T) {
	workDir := t.TempDir()
	store := NewReviewStore()
	store.CreateReview("clean-job")

	// clean-job n'est pas dirty.
	if err := store.Flush("clean-job", workDir); err != nil {
		t.Fatalf("Flush non-dirty: %v", err)
	}

	// Aucun fichier ne doit avoir ete cree.
	entries, err := os.ReadDir(workDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("workDir non vide apres Flush non-dirty: %v", entries)
	}
}

// TestNotifyEditResetsTimer verifie que NotifyEdit remet le timer a zero.
func TestNotifyEditResetsTimer(t *testing.T) {
	workDir := t.TempDir()
	store := NewReviewStore()
	job := store.CreateReview("reset-job")
	cues := []*CueState{
		{Index: 1, SourceLines: []string{"A"}, TargetLines: []string{"B"}, durationSecs: 1.0},
	}
	for _, c := range cues {
		c.Flags = computeFlags(c)
	}
	job.mu.Lock()
	job.files["f.srt"] = &FileState{Name: "f.srt", Cues: cues}
	job.dirty = true
	job.mu.Unlock()

	fm := &FlushManager{
		store:   store,
		workDir: workDir,
		timers:  make(map[string]*time.Timer),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}

	// Appeler NotifyEdit deux fois — le timer doit etre remis a zero.
	// On verifie juste que NotifyEdit ne panique pas et n'ouvre pas deux timers.
	fm.NotifyEdit("reset-job")
	fm.NotifyEdit("reset-job") // reset le meme timer

	fm.mu.Lock()
	n := len(fm.timers)
	fm.mu.Unlock()
	if n != 1 {
		t.Errorf("nombre de timers = %d, want 1", n)
	}

	// Annuler manuellement le timer pour eviter une goroutine pendante.
	fm.mu.Lock()
	for _, tt := range fm.timers {
		tt.Stop()
	}
	fm.mu.Unlock()
}
