package notify

import (
	"encoding/binary"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Windows toasts name the sound they play with an audio source: an
// ms-winsoundevent URI that selects one of the system's own sounds, or the URI
// of a file. These are the sounds of settings that name none of their own.
const (
	soundDefault = "ms-winsoundevent:Notification.Default"
	soundReminder = "ms-winsoundevent:Notification.Reminder"
	soundEvent   = "ms-winsoundevent:"
)

const silentAudio = `<audio silent="true"/>`

// sounds are the sounds the app saved in its settings: an audio source each,
// "" for the system's own.
var sounds struct {
	sync.RWMutex
	normal, high string
	lowPriority  bool
	muted        bool
}

// SetSounds sets the sounds notifications play. The app calls it with its
// settings, on start and whenever the user changes them.
func SetSounds(normal, high string, lowPriority, muted bool) {
	sounds.Lock()
	sounds.normal, sounds.high, sounds.lowPriority, sounds.muted = normal, high, lowPriority, muted
	sounds.Unlock()
}

// configuredSounds are the audio sources of a normal and of a high priority
// notification, whether the quiet priorities play one too, and whether the
// user turned every sound off.
func configuredSounds() (normal, high string, lowPriority, muted bool) {
	sounds.RLock()
	defer sounds.RUnlock()
	return sounds.normal, sounds.high, sounds.lowPriority, sounds.muted
}

// soundPlan is how a notification makes its noise: the audio element of its
// toast, and a WAV file the app plays itself.
//
// Windows only honours an audio element that names a file for the apps it
// installs as packages. This app is a plain executable, so a WAV is played here
// and the toast stays silent; a system event is first resolved to the file of
// the user's sound scheme, which also keeps the volume mixer's per app level
// working.
type soundPlan struct {
	Element string
	File    string
}

// soundPlanFor is the plan of the sound matched to a message's level, or a
// silent one.
func soundPlanFor(n Notification) soundPlan {
	normal, high, lowPriority, muted := configuredSounds()
	if muted {
		// The switch of the settings covers the sounds below, and the preview
		// too: it exists to turn the app quiet, not one message's way of
		// being loud.
		return soundPlan{Element: silentAudio}
	}
	if n.Sound != "" {
		// A preview plays the sound the user is about to save.
		return planSound(n.Sound, soundDefault)
	}
	switch n.Level {
	case LevelHigh:
		return planSound(high, soundReminder)
	case LevelSilent:
		if lowPriority {
			return planSound(normal, soundDefault)
		}
		return soundPlan{Element: silentAudio}
	}
	return planSound(normal, soundDefault)
}

// planSound decides how one setting makes its sound. A setting that names
// nothing playable, a path that was moved or deleted, or a file that is not a
// WAV falls back to fallback; an empty fallback means no sound at all.
func planSound(v, fallback string) soundPlan {
	if f := playableFile(v); f != "" {
		return soundPlan{Element: silentAudio, File: f}
	}
	if f := systemEventFile(v); f != "" {
		return soundPlan{Element: silentAudio, File: f}
	}
	v = strings.TrimSpace(os.ExpandEnv(v))
	if isAudioSource(v) {
		// A link, or a system event this machine has no file for: let the toast
		// try it rather than staying silent without a reason.
		return soundPlan{Element: audioTag(v)}
	}
	if fallback == "" {
		return soundPlan{Element: silentAudio}
	}
	return planSound(fallback, "")
}

// isAudioSource reports whether a setting is something a toast can name, as
// opposed to a file this app plays.
func isAudioSource(v string) bool {
	return strings.HasPrefix(v, soundEvent) ||
		strings.HasPrefix(v, "http://") ||
		strings.HasPrefix(v, "https://")
}

// playableFile is the absolute path of the WAV a setting names, or "" when the
// app has to play something else.
func playableFile(v string) string {
	v = strings.TrimSpace(os.ExpandEnv(v))
	if v == "" || isAudioSource(v) {
		return ""
	}
	abs, err := filepath.Abs(v)
	if err != nil {
		log.Printf("notify: sound %q: %v", v, err)
		return ""
	}
	fi, err := os.Stat(abs)
	if err != nil || fi.IsDir() {
		log.Printf("notify: sound %q is not a file, playing the system's instead", v)
		return ""
	}
	if !isWAV(abs) {
		log.Printf("notify: sound %q is not a WAV, playing the system's instead", v)
		return ""
	}
	return abs
}

// isWAV checks the header of a file, since the player refuses everything else
// without telling anyone why.
func isWAV(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [4]byte
	if _, err := f.Read(head[:]); err != nil {
		return false
	}
	switch string(head[:]) {
	case "RIFF", "RIFX", "RF64":
		return true
	}
	return false
}

// systemEventFile is the WAV of the sound scheme for a setting naming a system
// event such as ms-winsoundevent:Notification.IM.
func systemEventFile(v string) string {
	event, ok := strings.CutPrefix(strings.TrimSpace(os.ExpandEnv(v)), soundEvent)
	if !ok || event == "" {
		return ""
	}
	p := systemSoundFile(event)
	if p == "" {
		return ""
	}
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return ""
	}
	return p
}

// playSoundFile plays the WAV of a plan, logging a failure: a notification
// without its sound is otherwise silent about why.
func playSoundFile(file, id string) {
	if file == "" {
		return
	}
	if err := playSound(file); err != nil {
		log.Printf("notify: play %q: %v", file, err)
		return
	}
	startSound(id, file)
}

// Sound is the notification sound the app plays now: the message it belongs to,
// the file, and how long it lasts.
type Sound struct {
	ID       string
	Name     string
	File     string
	Duration time.Duration
	Started  time.Time
}

// playback is the sound of the moment, the window that shows it having to
// follow it: a notification's sound can outlast the notification by far.
var playback struct {
	sync.Mutex
	current   *Sound
	timer     *time.Timer
	gen       uint64
	listeners []func()
}

// OnSoundChange registers a function to run whenever what plays changes. The
// app uses it to show and hide the window of the sound; it runs on any
// goroutine and must not block.
func OnSoundChange(fn func()) {
	playback.Lock()
	playback.listeners = append(playback.listeners, fn)
	playback.Unlock()
}

// CurrentSound is the sound playing now, or nil when none is.
func CurrentSound() *Sound {
	playback.Lock()
	defer playback.Unlock()
	if playback.current == nil {
		return nil
	}
	s := *playback.current
	return &s
}

// StopSound stops the sound playing now, and the one playing after it, since a
// new sound starts on its own.
func StopSound() {
	playback.Lock()
	if playback.current == nil {
		playback.Unlock()
		return
	}
	playback.gen++
	playback.current = nil
	if playback.timer != nil {
		playback.timer.Stop()
		playback.timer = nil
	}
	listeners := append([]func(){}, playback.listeners...)
	playback.Unlock()
	stopSound()
	notifySoundChanged(listeners)
}

// startSound records a sound that just started, and the timer that ends it:
// the player stops a file by itself, so the window has to go when it does.
func startSound(id, file string) {
	snd := &Sound{ID: id, Name: filepath.Base(file), File: file, Duration: wavDuration(file), Started: time.Now()}
	playback.Lock()
	playback.current = snd
	playback.gen++
	gen := playback.gen
	if playback.timer != nil {
		playback.timer.Stop()
	}
	playback.timer = nil
	if snd.Duration > 0 {
		playback.timer = time.AfterFunc(snd.Duration+200*time.Millisecond, func() { endSound(gen) })
	}
	listeners := append([]func(){}, playback.listeners...)
	playback.Unlock()
	notifySoundChanged(listeners)
}

// endSound forgets a sound that reached its end, unless another replaced it.
func endSound(gen uint64) {
	playback.Lock()
	if playback.gen != gen || playback.current == nil {
		playback.Unlock()
		return
	}
	playback.current = nil
	playback.timer = nil
	listeners := append([]func(){}, playback.listeners...)
	playback.Unlock()
	notifySoundChanged(listeners)
}

func notifySoundChanged(listeners []func()) {
	for _, fn := range listeners {
		fn()
	}
}

// wavDuration is how long a WAV plays, for the window that counts it down. Zero
// when the file cannot be read as a WAV.
func wavDuration(path string) time.Duration {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var head [12]byte
	if _, err := io.ReadFull(f, head[:]); err != nil || string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return 0
	}
	byteRate := 0
	var chunk [8]byte
	for {
		if _, err := io.ReadFull(f, chunk[:]); err != nil {
			return 0
		}
		size := int(binary.LittleEndian.Uint32(chunk[4:8]))
		switch string(chunk[0:4]) {
		case "fmt ":
			if size < 12 || size > 4096 {
				return 0
			}
			format := make([]byte, size)
			if _, err := io.ReadFull(f, format); err != nil {
				return 0
			}
			byteRate = int(binary.LittleEndian.Uint32(format[8:12]))
		case "data":
			if byteRate <= 0 || size <= 0 {
				return 0
			}
			return time.Duration(int64(size) * int64(time.Second) / int64(byteRate))
		default:
			if _, err := f.Seek(int64(size), io.SeekCurrent); err != nil {
				return 0
			}
		}
		if size%2 == 1 {
			if _, err := f.Seek(1, io.SeekCurrent); err != nil {
				return 0
			}
		}
	}
}

// fileURI is the URI of a local file, for the callers that still name one.
func fileURI(abs string) string {
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func audioTag(src string) string {
	if src == "" {
		return silentAudio
	}
	return `<audio src="` + xmlEscape(src) + `" loop="false"/>`
}
