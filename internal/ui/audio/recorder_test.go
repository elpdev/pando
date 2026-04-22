package audio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordingCandidatesIncludeDarwin(t *testing.T) {
	candidates := recordingCandidates("darwin")
	if len(candidates) == 0 {
		t.Fatal("expected darwin recording candidates")
	}
	if candidates[0].name != "ffmpeg" {
		t.Fatalf("expected ffmpeg as first darwin recorder, got %q", candidates[0].name)
	}
}

func TestRecordingCandidatesIncludeLinux(t *testing.T) {
	candidates := recordingCandidates("linux")
	if len(candidates) == 0 {
		t.Fatal("expected linux recording candidates")
	}
	if candidates[0].name != "pw-record" {
		t.Fatalf("expected pw-record as first linux recorder, got %q", candidates[0].name)
	}
}

func TestHasRecordedAudioAcceptsNonEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "voice-note.wav")
	if err := os.WriteFile(path, []byte("RIFF"), 0o600); err != nil {
		t.Fatalf("write voice note: %v", err)
	}
	if !hasRecordedAudio(path) {
		t.Fatal("expected non-empty recording file to be accepted")
	}
}

func TestHasRecordedAudioRejectsMissingFile(t *testing.T) {
	if hasRecordedAudio(filepath.Join(t.TempDir(), "missing.wav")) {
		t.Fatal("expected missing recording file to be rejected")
	}
}

func TestAcceptableStopErrorIncludesTerminatedProcess(t *testing.T) {
	if !isAcceptableStopError(fakeError("signal: terminated")) {
		t.Fatal("expected terminated signal error to be accepted")
	}
	if !isAcceptableStopError(fakeError("exit status 255")) {
		t.Fatal("expected recorder exit status 255 to be accepted")
	}
}

type fakeError string

func (e fakeError) Error() string { return string(e) }
