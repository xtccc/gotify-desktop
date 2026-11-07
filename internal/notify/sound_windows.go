//go:build windows

package notify

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// systemSoundFile is the WAV the sound scheme gives a system event such as
// Notification.Default. The user's own choice (Current) wins over the scheme's
// default, so replacing a sound in the Sound control panel is honoured.
func systemSoundFile(event string) string {
	base := `AppEvents\Schemes\Apps\.Default\` + event
	for _, sub := range []string{`.Current`, `.Default`} {
		k, err := registry.OpenKey(registry.CURRENT_USER, base+`\`+sub, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("")
		k.Close()
		if err != nil || strings.TrimSpace(v) == "" {
			continue
		}
		if p := soundPath(v); p != "" {
			return p
		}
	}
	return ""
}

// soundPath is the absolute path of a sound of the scheme, which stores it as a
// path or as a name relative to the media directory.
func soundPath(v string) string {
	v = os.ExpandEnv(strings.TrimSpace(v))
	if !filepath.IsAbs(v) {
		v = filepath.Join(os.Getenv("SystemRoot"), "media", v)
	}
	return v
}

var (
	winmm         = syscall.NewLazyDLL("winmm.dll")
	procPlaySound = winmm.NewProc("PlaySoundW")
)

const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndFilename  = 0x00020000
)

// playSound plays a WAV without blocking. A new sound stops the one already
// playing, so a burst of notifications does not stack them.
func playSound(path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	ok, _, callErr := procPlaySound.Call(uintptr(unsafe.Pointer(p)), 0, sndAsync|sndFilename|sndNoDefault)
	runtime.KeepAlive(p)
	if ok != 0 {
		return nil
	}
	if errno, isErrno := callErr.(syscall.Errno); isErrno && errno != 0 {
		return errno
	}
	return errors.New("the audio device refused the file")
}

// stopSound ends the sound a notification started.
func stopSound() { procPlaySound.Call(0, 0, 0) }
