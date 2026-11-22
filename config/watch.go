package config

import (
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch calls onChange whenever the config file changes (debounced).
func Watch(path string, onChange func(*ChainConfig), stop <-chan struct{}) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := w.Add(path); err != nil {
		w.Close()
		return err
	}
	go func() {
		defer w.Close()
		var timer *time.Timer
		for {
			select {
			case <-stop:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(200*time.Millisecond, func() {
					if c, err := Load(path); err == nil {
						onChange(c)
					}
				})
			case <-w.Errors:
			}
		}
	}()
	return nil
}
