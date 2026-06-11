package main

import (
	"bytes"
	"strings"
	"testing"
)

// execRoot exécute la commande racine avec args et capture stdout.
func execRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestRootVersion(t *testing.T) {
	out, err := execRoot(t, "--version")
	if err != nil {
		t.Fatalf("--version a échoué: %v", err)
	}
	if !strings.Contains(out, "translai version") {
		t.Fatalf("sortie version inattendue: %q", out)
	}
}

func TestTranslateHelp(t *testing.T) {
	out, err := execRoot(t, "translate", "--help")
	if err != nil {
		t.Fatalf("translate --help a échoué: %v", err)
	}
	for _, flag := range []string{"--input", "--target", "--source", "--out-dir", "--provider"} {
		if !strings.Contains(out, flag) {
			t.Errorf("flag %q absent du help translate", flag)
		}
	}
}

// TestTranslateStubNotImplemented documente l'état Phase 0 : la commande existe
// mais renvoie une erreur tant que le pipeline (Phase 5) n'est pas branché.
// À SUPPRIMER en Phase 5 quand translate devient fonctionnel.
func TestTranslateStubNotImplemented(t *testing.T) {
	_, err := execRoot(t, "translate", "-i", "x.srt", "--target", "fr")
	if err == nil {
		t.Fatal("translate stub devrait renvoyer une erreur en Phase 0")
	}
}
