// Copyright 2018 The sphinx Authors
// Modified based on go-ethereum, which Copyright (C) 2014 The go-ethereum Authors.
//
// The sphinx is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The sphinx is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the sphinx. If not, see <http://www.gnu.org/licenses/>.

// +build darwin,!ios freebsd linux,!arm64 netbsd solaris

package keystore

import (
	"time"

	"github.com/shx-project/sphinx/common/log"
	"github.com/rjeczalik/notify"
)

type watcher struct {
	ac       *accountCache
	starting bool
	running  bool
	ev       chan notify.EventInfo
	quit     chan struct{}
}

func newWatcher(ac *accountCache) *watcher {
	return &watcher{
		ac:   ac,
		ev:   make(chan notify.EventInfo, 10),
		quit: make(chan struct{}),
	}
}

// starts the watcher loop in the background.
// Start a watcher in the background if that's not already in progress.
// The caller must hold w.ac.mu.
func (w *watcher) start() {
	if w.starting || w.running {
		return
	}
	w.starting = true
	go w.loop()
}

func (w *watcher) close() {
	close(w.quit)
}

func (w *watcher) loop() {
	defer func() {
		w.ac.mu.Lock()
		w.running = false
		w.starting = false
		w.ac.mu.Unlock()
	}()
	logger := log.New("path", w.ac.keydir)

	if err := notify.Watch(w.ac.keydir, w.ev, notify.All); err != nil {
		logger.Trace("Failed to watch keystore folder", "err", err)
		return
	}
	defer notify.Stop(w.ev)
	logger.Trace("Started watching keystore folder")
	defer logger.Trace("Stopped watching keystore folder")

	w.ac.mu.Lock()
	w.running = true
	w.ac.mu.Unlock()

	// Wait for file system events and reload.
	// When an event occurs, the reload call is delayed a bit so that
	// multiple events arriving quickly only cause a single reload.
	var (
		debounce         = time.NewTimer(0)
		debounceDuration = 500 * time.Millisecond
		rescanTriggered  = false
	)
	defer debounce.Stop()
	for {
		select {
		case <-w.quit:
			return
		case <-w.ev:
			// Trigger the scan (with delay), if not already triggered
			if !rescanTriggered {
				debounce.Reset(debounceDuration)
				rescanTriggered = true
			}
		case <-debounce.C:
			w.ac.scanAccounts()
			rescanTriggered = false
		}
	}
}