package notify

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMain starts from the sounds of an app that saved none.
func TestMain(m *testing.M) {
	SetSounds("", "", false)
	os.Exit(m.Run())
}

// useSounds sets the sounds of the settings for one test and restores the
// system's afterwards, so no test sees another's.
func useSounds(t *testing.T, normal, high string, lowPriority bool) {
	t.Helper()
	SetSounds(normal, high, lowPriority)
	t.Cleanup(func() { SetSounds("", "", false) })
}

// wavHeader is enough of a WAV to be recognised, without 44 bytes of silence in
// every test's temporary directory.
const wavHeader = "RIFF\x24\x08\x00\x00WAVEfmt "

// wav writes a file standing in for a WAV the settings point at.
func wav(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(wavHeader), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// onWindows runs f only where the app plays sound files itself.
func onWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the app plays sound files on Windows only")
	}
}

func TestSoundsDefaultUsesScheme(t *testing.T) {
	p := soundPlanFor(Notification{ID: "x", Level: LevelNormal})
	if !strings.HasPrefix(p.Element, "<audio") {
		t.Fatalf("%+v", p)
	}
	if runtime.GOOS != "windows" {
		if p.Element != audioTag(soundDefault) {
			t.Fatalf("%+v", p)
		}
		return
	}
	// A system event is resolved to the file of the scheme, and the app plays
	// it: the toast itself names no source.
	if p.File == "" || p.Element != silentAudio {
		t.Fatalf("%+v", p)
	}
	if !strings.HasSuffix(strings.ToLower(p.File), ".wav") {
		t.Fatalf("%+v", p)
	}
}

func TestSoundsFromFiles(t *testing.T) {
	normal, high := wav(t, "normal.wav"), wav(t, "high.wav")
	useSounds(t, normal, high, false)
	for level, want := range map[Level]string{LevelNormal: normal, LevelHigh: high} {
		p := soundPlanFor(Notification{ID: "x", Level: level})
		if p.File != want || p.Element != silentAudio {
			t.Fatalf("level %d: %+v", level, p)
		}
	}
}

func TestSoundsMissingFileFallsBack(t *testing.T) {
	useSounds(t, filepath.Join(t.TempDir(), "gone.wav"), "", false)
	p := soundPlanFor(Notification{ID: "x", Level: LevelNormal})
	if strings.Contains(p.Element, "gone.wav") || strings.Contains(p.File, "gone.wav") {
		t.Fatalf("%+v", p)
	}
	if runtime.GOOS != "windows" && p.Element != audioTag(soundDefault) {
		t.Fatalf("%+v", p)
	}
}

func TestSoundsNonWAVFallsBack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "track.mp3")
	if err := os.WriteFile(p, []byte("ID3\x04\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	useSounds(t, p, "", false)
	if plan := soundPlanFor(Notification{ID: "x", Level: LevelNormal}); plan.File == p {
		t.Fatalf("%+v", plan)
	}
}

func TestSoundsSystemEvent(t *testing.T) {
	useSounds(t, "ms-winsoundevent:Notification.IM", "https://example.com/chime.wav", false)
	if runtime.GOOS != "windows" {
		if p := soundPlanFor(Notification{ID: "x", Level: LevelNormal}); p.Element != audioTag("ms-winsoundevent:Notification.IM") {
			t.Fatalf("%+v", p)
		}
	} else if p := soundPlanFor(Notification{ID: "x", Level: LevelNormal}); !strings.HasSuffix(p.File, "Windows Notify Messaging.wav") || p.Element != silentAudio {
		// The scheme's file for the event, played by the app.
		t.Fatalf("%+v", p)
	}
	if p := soundPlanFor(Notification{ID: "x", Level: LevelHigh}); p.Element != audioTag("https://example.com/chime.wav") || p.File != "" {
		t.Fatalf("%+v", p)
	}
}

func TestToastXMLLowPrioritySounds(t *testing.T) {
	normal := wav(t, "low.wav")
	useSounds(t, normal, "", true)
	p := soundPlanFor(Notification{ID: "x", Level: LevelSilent})
	if p.File != normal || p.Element != silentAudio {
		t.Fatalf("%+v", p)
	}
}

func TestToastXMLLowPriorityStaysQuiet(t *testing.T) {
	useSounds(t, wav(t, "low.wav"), "", false)
	if p := soundPlanFor(Notification{ID: "x", Level: LevelSilent}); p.File != "" || p.Element != silentAudio {
		t.Fatalf("%+v", p)
	}
}

func TestToastXMLPreviewSound(t *testing.T) {
	useSounds(t, "ms-winsoundevent:Notification.Mail", "", false)
	p := wav(t, "preview.wav")
	plan := soundPlanFor(Notification{ID: "x", Level: LevelHigh, Sound: p})
	if plan.File != p {
		t.Fatalf("%+v", plan)
	}
	if d := parseToast(t, buildToastXML(Notification{ID: "x", Level: LevelHigh, Sound: p})); d.Duration != "long" {
		t.Fatalf("%+v", d)
	}
}

func TestToastXMLPreviewMissingSound(t *testing.T) {
	useSounds(t, "", "", false)
	plan := soundPlanFor(Notification{ID: "x", Level: LevelNormal, Sound: "/nowhere/gone.wav"})
	if runtime.GOOS != "windows" && plan.Element != audioTag(soundDefault) {
		t.Fatalf("%+v", plan)
	}
	if runtime.GOOS == "windows" && plan.File == "" {
		t.Fatalf("%+v", plan)
	}
}

// TestPlayableFileIsAbsolute guards the player, which reads the path as given.
func TestPlayableFileIsAbsolute(t *testing.T) {
	p := wav(t, "sound.wav")
	if got := playableFile(p); got != filepath.Clean(p) {
		t.Fatalf("%q, want %q", got, p)
	}
}

func TestSoundPathRelativeToMedia(t *testing.T) {
	onWindows(t)
	if got := soundPath(`Windows Notify System Generic.wav`); !filepath.IsAbs(got) || !strings.HasSuffix(filepath.ToSlash(got), "/media/Windows Notify System Generic.wav") {
		t.Fatalf("%q", got)
	}
}
