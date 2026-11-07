//go:build !windows

package notify

import "errors"

// systemSoundFile names no file: elsewhere the toast plays the sound itself.
func systemSoundFile(event string) string { return "" }

func playSound(path string) error { return errors.New("playing a sound file is not supported here") }

func stopSound() {}
