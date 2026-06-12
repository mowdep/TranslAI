package server

import (
	"testing"
)

// makeTestJob cree un ReviewJob avec quelques cues de test.
func makeTestJob(t *testing.T, store ReviewStore, jobID string) *ReviewJob {
	t.Helper()
	job := store.CreateReview(jobID)
	cues := []*CueState{
		{
			Index:        1,
			SourceLines:  []string{"Hello world"},
			TargetLines:  []string{"Bonjour monde"},
			durationSecs: 2.0,
		},
		{
			Index:        2,
			SourceLines:  []string{"How are you?"},
			TargetLines:  []string{"How are you?"},
			durationSecs: 1.5,
		},
		{
			Index:        3,
			SourceLines:  []string{"This is a test"},
			TargetLines:  []string{},
			durationSecs: 1.0,
		},
	}
	for _, c := range cues {
		c.Flags = computeFlags(c)
	}

	job.mu.Lock()
	job.files["test.srt"] = &FileState{Name: "test.srt", Cues: cues}
	job.mu.Unlock()
	return job
}

// TestUpdateCueAndGetCues verifie que UpdateCue modifie la cue et que GetCues renvoie l'etat a jour.
func TestUpdateCueAndGetCues(t *testing.T) {
	store := NewReviewStore()
	makeTestJob(t, store, "job1")

	// Modifier la cue 1.
	newLines := []string{"Salut le monde"}
	if err := store.UpdateCue("job1", "test.srt", 1, newLines); err != nil {
		t.Fatalf("UpdateCue: %v", err)
	}

	cues, err := store.GetCues("job1", "test.srt")
	if err != nil {
		t.Fatalf("GetCues: %v", err)
	}
	if len(cues) != 3 {
		t.Fatalf("len(cues) = %d, want 3", len(cues))
	}
	if cues[0].TargetLines[0] != "Salut le monde" {
		t.Errorf("TargetLines[0] = %q, want %q", cues[0].TargetLines[0], "Salut le monde")
	}
}

// TestUpdateCueDirty verifie que l'edit marque le job dirty.
func TestUpdateCueDirty(t *testing.T) {
	store := NewReviewStore()
	job := makeTestJob(t, store, "job2")

	// Verifier que dirty est false initialement.
	job.mu.RLock()
	d := job.dirty
	job.mu.RUnlock()
	if d {
		t.Error("job.dirty devrait etre false avant l'edit")
	}

	// Modifier une cue.
	_ = store.UpdateCue("job2", "test.srt", 1, []string{"modifie"})

	job.mu.RLock()
	d = job.dirty
	job.mu.RUnlock()
	if !d {
		t.Error("job.dirty devrait etre true apres l'edit")
	}
}

// TestFlagEcho verifie FlagEcho (cible == source).
func TestFlagEcho(t *testing.T) {
	c := &CueState{
		Index:       1,
		SourceLines: []string{"Hello world"},
		TargetLines: []string{"Hello world"},
	}
	flags := computeFlags(c)
	if !hasFlag(flags, FlagEcho) {
		t.Errorf("FlagEcho attendu pour source == cible, flags = %v", flags)
	}
}

// TestFlagEmpty verifie FlagEmpty (cible vide).
func TestFlagEmpty(t *testing.T) {
	c := &CueState{
		Index:       1,
		SourceLines: []string{"Hello"},
		TargetLines: []string{""},
	}
	flags := computeFlags(c)
	if !hasFlag(flags, FlagEmpty) {
		t.Errorf("FlagEmpty attendu pour cible vide, flags = %v", flags)
	}
	// FlagEcho ne doit pas etre present quand la cible est vide.
	if hasFlag(flags, FlagEcho) {
		t.Error("FlagEcho ne doit pas etre present quand cible est vide")
	}
}

// TestFlagRatioLow verifie FlagRatioLow (ratio < 0.4).
func TestFlagRatioLow(t *testing.T) {
	// Source : 20 chars, Cible : 5 chars → ratio 0.25 < 0.4
	c := &CueState{
		Index:       1,
		SourceLines: []string{"This is a long sentence"},
		TargetLines: []string{"Ok"},
	}
	flags := computeFlags(c)
	if !hasFlag(flags, FlagRatioLow) {
		t.Errorf("FlagRatioLow attendu, flags = %v", flags)
	}
}

// TestFlagRatioHigh verifie FlagRatioHigh (ratio > 2.5).
func TestFlagRatioHigh(t *testing.T) {
	// Source : 2 chars, Cible : 30 chars → ratio > 2.5
	c := &CueState{
		Index:       1,
		SourceLines: []string{"Hi"},
		TargetLines: []string{"Bonjour a tous mes amis tres chers"},
	}
	flags := computeFlags(c)
	if !hasFlag(flags, FlagRatioHigh) {
		t.Errorf("FlagRatioHigh attendu, flags = %v", flags)
	}
}

// TestFlagLineMismatch verifie FlagLineMismatch (nb lignes different).
func TestFlagLineMismatch(t *testing.T) {
	c := &CueState{
		Index:       1,
		SourceLines: []string{"Line 1", "Line 2"},
		TargetLines: []string{"Ligne 1"},
	}
	flags := computeFlags(c)
	if !hasFlag(flags, FlagLineMismatch) {
		t.Errorf("FlagLineMismatch attendu, flags = %v", flags)
	}
}

// TestFlagFallback verifie FlagFallback (runtime).
func TestFlagFallback(t *testing.T) {
	c := &CueState{
		Index:        1,
		SourceLines:  []string{"Hello"},
		TargetLines:  []string{"Bonjour"},
		FallbackUsed: true,
	}
	flags := computeFlags(c)
	if !hasFlag(flags, FlagFallback) {
		t.Errorf("FlagFallback attendu, flags = %v", flags)
	}
}

// TestFlagCPSHigh verifie FlagCPSHigh (chars/s > 20).
func TestFlagCPSHigh(t *testing.T) {
	// 50 chars en 1 seconde → 50 cps > 20
	c := &CueState{
		Index:        1,
		SourceLines:  []string{"Short"},
		TargetLines:  []string{"Ceci est une phrase avec beaucoup de caracteres"},
		durationSecs: 1.0,
	}
	flags := computeFlags(c)
	if !hasFlag(flags, FlagCPSHigh) {
		t.Errorf("FlagCPSHigh attendu, flags = %v", flags)
	}
}

// TestFlagLongLine verifie FlagLongLine (> 42 runes).
func TestFlagLongLine(t *testing.T) {
	c := &CueState{
		Index:       1,
		SourceLines: []string{"Short"},
		TargetLines: []string{"Ceci est une ligne vraiment tres longue qui depasse la limite de quarante-deux caracteres"},
	}
	flags := computeFlags(c)
	if !hasFlag(flags, FlagLongLine) {
		t.Errorf("FlagLongLine attendu, flags = %v", flags)
	}
}

// TestNoFlagsForNormalCue verifie qu'une cue normale n'a pas de flags.
func TestNoFlagsForNormalCue(t *testing.T) {
	c := &CueState{
		Index:        1,
		SourceLines:  []string{"Hello world"},
		TargetLines:  []string{"Bonjour monde"},
		durationSecs: 3.0,
	}
	flags := computeFlags(c)
	if len(flags) != 0 {
		t.Errorf("aucun flag attendu pour une cue normale, flags = %v", flags)
	}
}

// TestUpdateCueRecalculatesFlags verifie que les flags sont recalcules apres UpdateCue.
func TestUpdateCueRecalculatesFlags(t *testing.T) {
	store := NewReviewStore()
	job := store.CreateReview("job-flags")
	cues := []*CueState{
		{
			Index:       1,
			SourceLines: []string{"Hello world"},
			TargetLines: []string{"Hello world"}, // echo
		},
	}
	cues[0].Flags = computeFlags(cues[0])
	job.mu.Lock()
	job.files["test.srt"] = &FileState{Name: "test.srt", Cues: cues}
	job.mu.Unlock()

	// Verifier que FlagEcho est present.
	if !hasFlag(cues[0].Flags, FlagEcho) {
		t.Fatal("FlagEcho attendu initialement")
	}

	// Corriger la traduction → plus de FlagEcho.
	_ = store.UpdateCue("job-flags", "test.srt", 1, []string{"Bonjour monde"})

	updated, _ := store.GetCues("job-flags", "test.srt")
	if hasFlag(updated[0].Flags, FlagEcho) {
		t.Error("FlagEcho ne doit plus etre present apres correction")
	}
}

// TestUpdateCueNotFound verifie les erreurs quand le job/fichier/cue est introuvable.
func TestUpdateCueNotFound(t *testing.T) {
	store := NewReviewStore()

	if err := store.UpdateCue("inexistant", "test.srt", 1, []string{"x"}); err == nil {
		t.Error("devrait retourner une erreur pour job inexistant")
	}

	makeTestJob(t, store, "job3")
	if err := store.UpdateCue("job3", "inexistant.srt", 1, []string{"x"}); err == nil {
		t.Error("devrait retourner une erreur pour fichier inexistant")
	}
	if err := store.UpdateCue("job3", "test.srt", 999, []string{"x"}); err == nil {
		t.Error("devrait retourner une erreur pour cue inexistante")
	}
}

// TestAllDirtyJobs verifie que AllDirtyJobs retourne les bons IDs.
func TestAllDirtyJobs(t *testing.T) {
	store := NewReviewStore()
	makeTestJob(t, store, "clean1")
	makeTestJob(t, store, "dirty1")

	// Modifier un job pour le rendre dirty.
	_ = store.UpdateCue("dirty1", "test.srt", 1, []string{"modifie"})

	dirty := store.AllDirtyJobs()
	if len(dirty) != 1 || dirty[0] != "dirty1" {
		t.Errorf("AllDirtyJobs = %v, want [dirty1]", dirty)
	}
}

// TestCPSNotHighWhenLongDuration verifie que CPS est OK pour duree longue.
func TestCPSNotHighWhenLongDuration(t *testing.T) {
	// 10 chars en 5 secondes → 2 cps < 20
	c := &CueState{
		Index:        1,
		SourceLines:  []string{"Short"},
		TargetLines:  []string{"Bonjour"},
		durationSecs: 5.0,
	}
	flags := computeFlags(c)
	if hasFlag(flags, FlagCPSHigh) {
		t.Errorf("FlagCPSHigh ne doit pas etre present, flags = %v", flags)
	}
}

// TestFlagNoCPSWhenZeroDuration verifie que FlagCPSHigh est absent si duree == 0.
func TestFlagNoCPSWhenZeroDuration(t *testing.T) {
	c := &CueState{
		Index:        1,
		SourceLines:  []string{"Short"},
		TargetLines:  []string{"Ceci est une phrase avec beaucoup de caracteres pour tester"},
		durationSecs: 0,
	}
	flags := computeFlags(c)
	if hasFlag(flags, FlagCPSHigh) {
		t.Errorf("FlagCPSHigh ne doit pas etre present si duree == 0, flags = %v", flags)
	}
}

// TestGetReview verifie CreateReview et GetReview.
func TestGetReview(t *testing.T) {
	store := NewReviewStore()
	store.CreateReview("myjob")

	j, ok := store.GetReview("myjob")
	if !ok {
		t.Fatal("GetReview: job introuvable")
	}
	if j.ID != "myjob" {
		t.Errorf("j.ID = %q, want %q", j.ID, "myjob")
	}

	_, ok = store.GetReview("inexistant")
	if ok {
		t.Error("GetReview: devrait retourner false pour job inexistant")
	}
}

// TestMarkDirty verifie MarkDirty.
func TestMarkDirty(t *testing.T) {
	store := NewReviewStore()
	job := store.CreateReview("jobdirty")

	job.mu.RLock()
	d := job.dirty
	job.mu.RUnlock()
	if d {
		t.Error("job.dirty devrait etre false initialement")
	}

	store.MarkDirty("jobdirty")

	job.mu.RLock()
	d = job.dirty
	job.mu.RUnlock()
	if !d {
		t.Error("job.dirty devrait etre true apres MarkDirty")
	}
}

// hasFlag est un helper pour les tests.
func hasFlag(flags []Flag, f Flag) bool {
	for _, fl := range flags {
		if fl == f {
			return true
		}
	}
	return false
}

