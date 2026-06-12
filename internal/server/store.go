package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gabrielfareau/translai/internal/srt"
)

// Flag identifie un type d'anomalie detectee sur une cue traduite.
type Flag string

const (
	FlagEcho         Flag = "echo"          // cible == source
	FlagEmpty        Flag = "empty"         // cible vide
	FlagRatioLow     Flag = "ratio_low"     // ratio longueur < 0.4
	FlagRatioHigh    Flag = "ratio_high"    // ratio longueur > 2.5
	FlagLineMismatch Flag = "line_mismatch" // nb lignes source != cible
	FlagFallback     Flag = "fallback"      // pipeline a utilise le fallback cue-par-cue
	FlagCPSHigh      Flag = "cps_high"      // chars/s > 20 sur la duree de la cue
	FlagLongLine     Flag = "long_line"     // au moins une ligne > 42 runes
)

// CueState est l'etat mutable d'une cue traduite (en memoire uniquement).
type CueState struct {
	Index        int
	SourceLines  []string
	TargetLines  []string // editable
	Flags        []Flag   // calcules a la volee, non persistes
	FallbackUsed bool     // metadonnee runtime du pipeline, persiste dans sidecar
	Reviewed     bool     // marque par l'utilisateur, persiste dans sidecar
	// durationSecs est la duree de la cue en secondes (pour FlagCPSHigh).
	durationSecs float64
	// startMS / endMS pour reconstruire les timestamps dans le SRT flush.
	startMS int64
	endMS   int64
}

// FileState contient toutes les cues traduites d'un fichier.
type FileState struct {
	Name string
	Cues []*CueState
}

// ReviewJob est un job mutable en memoire.
type ReviewJob struct {
	ID    string
	mu    sync.RWMutex
	files map[string]*FileState
	dirty bool // true si modification non encore flushee sur disque
}

// ReviewStore stocke les jobs de review (in-memory + write-behind disque).
type ReviewStore interface {
	CreateReview(id string) *ReviewJob
	GetReview(id string) (*ReviewJob, bool)
	UpdateCue(jobID, file string, index int, targetLines []string) error
	GetCues(jobID, file string) ([]*CueState, error)
	MarkDirty(jobID string)
	FlushAll(workDir string) error
	Flush(jobID, workDir string) error
	LoadFromDisk(workDir string) error
	// AllDirtyJobs retourne les IDs des jobs dirty.
	AllDirtyJobs() []string
}

// memReviewStore est l'implementation in-memory de ReviewStore.
type memReviewStore struct {
	mu   sync.RWMutex
	jobs map[string]*ReviewJob
}

// NewReviewStore cree un nouveau ReviewStore in-memory.
func NewReviewStore() ReviewStore {
	return &memReviewStore{
		jobs: make(map[string]*ReviewJob),
	}
}

func (s *memReviewStore) CreateReview(id string) *ReviewJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := &ReviewJob{
		ID:    id,
		files: make(map[string]*FileState),
	}
	s.jobs[id] = j
	return j
}

func (s *memReviewStore) GetReview(id string) (*ReviewJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

func (s *memReviewStore) UpdateCue(jobID, file string, index int, targetLines []string) error {
	s.mu.RLock()
	j, ok := s.jobs[jobID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("review: job %q introuvable", jobID)
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	fs, ok := j.files[file]
	if !ok {
		return fmt.Errorf("review: fichier %q introuvable dans job %q", file, jobID)
	}
	var cue *CueState
	for _, c := range fs.Cues {
		if c.Index == index {
			cue = c
			break
		}
	}
	if cue == nil {
		return fmt.Errorf("review: cue %d introuvable", index)
	}
	cue.TargetLines = targetLines
	cue.Flags = computeFlags(cue)
	j.dirty = true
	return nil
}

func (s *memReviewStore) GetCues(jobID, file string) ([]*CueState, error) {
	s.mu.RLock()
	j, ok := s.jobs[jobID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("review: job %q introuvable", jobID)
	}

	j.mu.RLock()
	defer j.mu.RUnlock()
	fs, ok := j.files[file]
	if !ok {
		return nil, fmt.Errorf("review: fichier %q introuvable dans job %q", file, jobID)
	}
	// Retourner une copie superficielle.
	out := make([]*CueState, len(fs.Cues))
	copy(out, fs.Cues)
	return out, nil
}

func (s *memReviewStore) MarkDirty(jobID string) {
	s.mu.RLock()
	j, ok := s.jobs[jobID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	j.mu.Lock()
	j.dirty = true
	j.mu.Unlock()
}

func (s *memReviewStore) AllDirtyJobs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []string
	for id, j := range s.jobs {
		j.mu.RLock()
		d := j.dirty
		j.mu.RUnlock()
		if d {
			ids = append(ids, id)
		}
	}
	return ids
}

// sidecarCue est la representation JSON d'une cue dans le sidecar.
type sidecarCue struct {
	Index        int  `json:"index"`
	FallbackUsed bool `json:"fallback_used"`
	Reviewed     bool `json:"reviewed"`
}

// sidecarFile est le contenu complet du sidecar.json.
type sidecarFile struct {
	Cues []sidecarCue `json:"cues"`
}

// cueSnapshot est une copie immuable d'une CueState pour l'ecriture disque.
type cueSnapshot struct {
	index        int
	sourceLines  []string
	targetLines  []string
	fallbackUsed bool
	reviewed     bool
	durationSecs float64
	startMS      int64
	endMS        int64
}

// fileSnapshot est une copie immuable d'un FileState pour l'ecriture disque.
type fileSnapshot struct {
	name string
	cues []cueSnapshot
}

// snapshotJob retourne une copie des donnees du job sous mutex, et passe dirty a false.
// Retourne nil si le job n'est pas dirty.
func snapshotJob(j *ReviewJob) []fileSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.dirty {
		return nil
	}
	snapshots := make([]fileSnapshot, 0, len(j.files))
	for fname, fs := range j.files {
		snap := fileSnapshot{name: fname}
		for _, c := range fs.Cues {
			snap.cues = append(snap.cues, cueSnapshot{
				index:        c.Index,
				sourceLines:  append([]string(nil), c.SourceLines...),
				targetLines:  append([]string(nil), c.TargetLines...),
				fallbackUsed: c.FallbackUsed,
				reviewed:     c.Reviewed,
				durationSecs: c.durationSecs,
				startMS:      c.startMS,
				endMS:        c.endMS,
			})
		}
		snapshots = append(snapshots, snap)
	}
	j.dirty = false
	return snapshots
}

// FlushAll ecrit tous les jobs dirty sur disque.
// Snapshot sous mutex, write hors mutex.
func (s *memReviewStore) FlushAll(workDir string) error {
	s.mu.RLock()
	ids := make([]string, 0, len(s.jobs))
	for id := range s.jobs {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	var errs []string
	for _, id := range ids {
		if err := s.Flush(id, workDir); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("review: flush errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Flush ecrit un job specifique sur disque si dirty.
// Snapshot sous mutex, write hors mutex.
func (s *memReviewStore) Flush(jobID, workDir string) error {
	s.mu.RLock()
	j, ok := s.jobs[jobID]
	s.mu.RUnlock()
	if !ok {
		return nil
	}

	snapshots := snapshotJob(j)
	if snapshots == nil {
		return nil
	}

	// Write hors mutex.
	for _, snap := range snapshots {
		dir := filepath.Join(workDir, jobID, snap.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("review: mkdir %s: %w", dir, err)
		}

		// Ecrire le sidecar.json.
		sc := sidecarFile{}
		for _, c := range snap.cues {
			sc.Cues = append(sc.Cues, sidecarCue{
				Index:        c.index,
				FallbackUsed: c.fallbackUsed,
				Reviewed:     c.reviewed,
			})
		}
		scData, err := json.MarshalIndent(sc, "", "  ")
		if err != nil {
			return fmt.Errorf("review: sidecar marshal: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sidecar.json"), scData, 0o644); err != nil {
			return fmt.Errorf("review: sidecar write: %w", err)
		}

		// Ecrire le SRT canonique.
		srtPath := filepath.Join(dir, snap.name+".srt")
		if err := writeSRTFromCues(srtPath, snap.cues); err != nil {
			return fmt.Errorf("review: srt write: %w", err)
		}
	}
	return nil
}

// writeSRTFromCues ecrit un SRT canonique depuis les cues snapshot.
func writeSRTFromCues(path string, cues []cueSnapshot) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for i, c := range cues {
		if i > 0 {
			fmt.Fprintln(f)
		}
		fmt.Fprintf(f, "%d\n", c.index)
		fmt.Fprintf(f, "%s --> %s\n", formatSRTTime(c.startMS), formatSRTTime(c.endMS))
		for _, line := range c.targetLines {
			fmt.Fprintln(f, line)
		}
	}
	return nil
}

// formatSRTTime convertit des millisecondes en format SRT HH:MM:SS,mmm.
func formatSRTTime(ms int64) string {
	h := ms / 3_600_000
	ms -= h * 3_600_000
	m := ms / 60_000
	ms -= m * 60_000
	s := ms / 1_000
	ms -= s * 1_000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

// LoadFromDisk restaure les jobs depuis le repertoire workDir.
// Recalcule les flags deterministiques apres chargement.
func (s *memReviewStore) LoadFromDisk(workDir string) error {
	entries, err := os.ReadDir(workDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("review: readdir %s: %w", workDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		jobID := entry.Name()
		job := s.CreateReview(jobID)

		fileEntries, err := os.ReadDir(filepath.Join(workDir, jobID))
		if err != nil {
			continue
		}

		for _, fe := range fileEntries {
			if !fe.IsDir() {
				continue
			}
			fileName := fe.Name()
			fileDir := filepath.Join(workDir, jobID, fileName)

			// Lire le sidecar.json.
			var sc sidecarFile
			scData, err := os.ReadFile(filepath.Join(fileDir, "sidecar.json"))
			if err == nil {
				_ = json.Unmarshal(scData, &sc)
			}

			// Lire le SRT traduit.
			srtPath := filepath.Join(fileDir, fileName+".srt")
			var cues []*CueState
			srtData, err := os.ReadFile(srtPath)
			if err == nil {
				doc, parseErr := srt.Parse(strings.NewReader(string(srtData)))
				if parseErr == nil {
					cues = make([]*CueState, len(doc.Cues))
					for i, c := range doc.Cues {
						dur := c.End.Seconds() - c.Start.Seconds()
						cs := &CueState{
							Index:        c.Index,
							SourceLines:  append([]string(nil), c.Lines...),
							TargetLines:  append([]string(nil), c.Lines...),
							durationSecs: dur,
							startMS:      c.Start.Milliseconds(),
							endMS:        c.End.Milliseconds(),
						}
						// Appliquer les metadonnees du sidecar.
						for _, scc := range sc.Cues {
							if scc.Index == c.Index {
								cs.FallbackUsed = scc.FallbackUsed
								cs.Reviewed = scc.Reviewed
								break
							}
						}
						cs.Flags = computeFlags(cs)
						cues[i] = cs
					}
				}
			}

			job.mu.Lock()
			job.files[fileName] = &FileState{Name: fileName, Cues: cues}
			job.mu.Unlock()
		}
	}
	return nil
}

// AddFileToReviewJob ajoute les cues d'un fichier traduit a un job de review.
// sourceCues vient du document source, targetCues du document traduit,
// fallbackFlags indique les cues ou le fallback a ete utilise.
func AddFileToReviewJob(job *ReviewJob, fileName string, sourceCues []srt.Cue, targetCues []srt.Cue, fallbackFlags []bool) {
	cues := make([]*CueState, len(targetCues))
	for i, tc := range targetCues {
		var srcLines []string
		if i < len(sourceCues) {
			srcLines = append([]string(nil), sourceCues[i].Lines...)
		}
		dur := tc.End.Seconds() - tc.Start.Seconds()
		fallback := false
		if fallbackFlags != nil && i < len(fallbackFlags) {
			fallback = fallbackFlags[i]
		}
		cs := &CueState{
			Index:        tc.Index,
			SourceLines:  srcLines,
			TargetLines:  append([]string(nil), tc.Lines...),
			FallbackUsed: fallback,
			durationSecs: dur,
			startMS:      tc.Start.Milliseconds(),
			endMS:        tc.End.Milliseconds(),
		}
		cs.Flags = computeFlags(cs)
		cues[i] = cs
	}

	job.mu.Lock()
	job.files[fileName] = &FileState{Name: fileName, Cues: cues}
	job.dirty = true
	job.mu.Unlock()
}

// computeFlags calcule les flags deterministiques pour une cue.
// FlagFallback est preserve depuis CueState.FallbackUsed (charge depuis sidecar).
func computeFlags(c *CueState) []Flag {
	var flags []Flag

	target := strings.Join(c.TargetLines, " ")
	source := strings.Join(c.SourceLines, " ")

	// FlagEmpty : cible vide.
	if strings.Join(c.TargetLines, "") == "" {
		flags = append(flags, FlagEmpty)
	} else {
		// FlagEcho : cible == source.
		if target == source && source != "" {
			flags = append(flags, FlagEcho)
		}

		// FlagRatioLow / FlagRatioHigh.
		srcLen := len(source)
		tgtLen := len(target)
		if srcLen > 0 {
			ratio := float64(tgtLen) / float64(srcLen)
			if ratio < 0.4 {
				flags = append(flags, FlagRatioLow)
			} else if ratio > 2.5 {
				flags = append(flags, FlagRatioHigh)
			}
		}
	}

	// FlagLineMismatch : nb lignes source != cible.
	if len(c.SourceLines) != len(c.TargetLines) {
		flags = append(flags, FlagLineMismatch)
	}

	// FlagFallback : runtime, charge depuis sidecar.
	if c.FallbackUsed {
		flags = append(flags, FlagFallback)
	}

	// FlagCPSHigh : chars/s > 20.
	if c.durationSecs > 0 {
		tgtChars := utf8.RuneCountInString(strings.Join(c.TargetLines, ""))
		cps := float64(tgtChars) / c.durationSecs
		if cps > 20 {
			flags = append(flags, FlagCPSHigh)
		}
	}

	// FlagLongLine : au moins une ligne > 42 runes.
	for _, line := range c.TargetLines {
		if utf8.RuneCountInString(line) > 42 {
			flags = append(flags, FlagLongLine)
			break
		}
	}

	return flags
}
