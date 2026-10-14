// Package tasks keeps the running and recent background tasks (scans, DAT downloads...) and
// tells subscribers when they change, so the interface can show them live.
//
// Adapted from the task log of Switch Library Manager Web by the same author.
package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// kinds of tasks
const (
	KindScan      = "scan"
	KindDatSync   = "datsync"
	KindDatImport = "datimport"
	KindOrganize  = "organize"
)

// what started a task
const (
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
	TriggerWatcher  = "watcher"
	TriggerStartup  = "startup"
	TriggerSettings = "settings"
)

// task results
const (
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusWarning = "warning"
	StatusFailed  = "failed"
)

// problems reported by tasks; the interface translates them
const (
	NoteInterrupted = "Interrupted: the app stopped while the task was running."
	NoteScanFailed  = "The library could not be scanned."
	NoteUnreadable  = "Some files could not be read."
	NoteDatFailed   = "A DAT could not be downloaded."
	NoteNoLibrary   = "The folder of a library cannot be opened."
	NoteSkipped     = "Some files were left as they were."
	NoteOrganize    = "The files could not be organized."
)

const (
	filename = "tasks.json"
	// finished tasks kept in the history; failed tasks stay until they are dismissed
	maxFinished = 30
)

// Task is one run of background work.
type Task struct {
	ID       int64     `json:"id"`
	Kind     string    `json:"kind"`
	Trigger  string    `json:"trigger"`
	Status   string    `json:"status"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
	Current  int       `json:"current"`
	Total    int       `json:"total"`
	Message  string    `json:"message"`
	Error    *Note     `json:"error,omitempty"`
	Warnings []Note    `json:"warnings,omitempty"`
	// results, depending on the kind
	Files int `json:"files,omitempty"`
	Games int `json:"games,omitempty"`
}

// Note is a problem of a task: Text is an English sentence the interface translates, Detail
// the technical reason, shown as is.
type Note struct {
	Text   string `json:"text"`
	Detail string `json:"detail,omitempty"`
}

// Log keeps the running and recent tasks.
type Log struct {
	mutex       sync.Mutex
	path        string
	tasks       []*Task // newest first
	nextID      int64
	version     int64
	subscribers map[chan int64]struct{}
}

// Load reads the history saved in the data folder; tasks that were running when the app
// stopped are marked as failed. An empty folder keeps the log in memory only.
func Load(dataFolder string) *Log {
	log := &Log{subscribers: map[chan int64]struct{}{}}
	if dataFolder == "" {
		return log
	}
	log.path = filepath.Join(dataFolder, filename)
	if data, err := os.ReadFile(log.path); err == nil {
		saved := []*Task{}
		if json.Unmarshal(data, &saved) == nil {
			log.tasks = saved
		}
	}
	for _, task := range log.tasks {
		if task.ID >= log.nextID {
			log.nextID = task.ID
		}
		if task.Status == StatusRunning {
			task.Status = StatusFailed
			task.Error = &Note{Text: NoteInterrupted}
			task.Finished = task.Started
		}
	}
	return log
}

// Start records a new running task.
func (l *Log) Start(kind string, trigger string) int64 {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.nextID++
	task := &Task{ID: l.nextID, Kind: kind, Trigger: trigger, Status: StatusRunning, Started: time.Now(), Total: -1, Current: -1}
	l.tasks = append([]*Task{task}, l.tasks...)
	l.changed(false)
	return task.ID
}

// Progress updates a running task; progress is not saved to disk.
func (l *Log) Progress(id int64, current int, total int, message string) {
	l.update(id, false, func(task *Task) {
		task.Current, task.Total, task.Message = current, total, message
	})
}

// Warn adds a problem that did not stop the task.
func (l *Log) Warn(id int64, text string, detail string) {
	l.update(id, false, func(task *Task) {
		// the same problem many times is shown once
		for _, w := range task.Warnings {
			if w.Text == text && w.Detail == detail {
				return
			}
		}
		if len(task.Warnings) < 50 {
			task.Warnings = append(task.Warnings, Note{Text: text, Detail: detail})
		}
	})
}

// SetResult records what the task found.
func (l *Log) SetResult(id int64, files int, games int) {
	l.update(id, false, func(task *Task) {
		task.Files, task.Games = files, games
	})
}

// Finish ends a task; a failure makes it failed, warnings make it a warning.
func (l *Log) Finish(id int64, failure *Note) {
	l.update(id, true, func(task *Task) {
		task.Finished = time.Now()
		task.Message = ""
		switch {
		case failure != nil:
			task.Status = StatusFailed
			task.Error = failure
		case len(task.Warnings) > 0:
			task.Status = StatusWarning
		default:
			task.Status = StatusSuccess
		}
	})
}

// Dismiss removes a finished task from the history.
func (l *Log) Dismiss(id int64) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for i, task := range l.tasks {
		if task.ID == id && task.Status != StatusRunning {
			l.tasks = append(l.tasks[:i], l.tasks[i+1:]...)
			l.changed(true)
			return true
		}
	}
	return false
}

// Running reports whether a task of this kind is running.
func (l *Log) Running(kind string) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for _, task := range l.tasks {
		if task.Status == StatusRunning && (kind == "" || task.Kind == kind) {
			return true
		}
	}
	return false
}

// ClearFinished removes every finished task, failed ones included.
func (l *Log) ClearFinished() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	running := []*Task{}
	for _, task := range l.tasks {
		if task.Status == StatusRunning {
			running = append(running, task)
		}
	}
	l.tasks = running
	l.changed(true)
}

// Snapshot returns a copy of the tasks, newest first.
func (l *Log) Snapshot() []Task {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	out := make([]Task, 0, len(l.tasks))
	for _, task := range l.tasks {
		copied := *task
		copied.Warnings = append([]Note(nil), task.Warnings...)
		out = append(out, copied)
	}
	return out
}

// Subscribe returns a channel that receives the new version after every change, and a
// function to stop. Changes are coalesced: a slow reader only gets the latest version.
func (l *Log) Subscribe() (<-chan int64, func()) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	ch := make(chan int64, 1)
	l.subscribers[ch] = struct{}{}
	return ch, func() {
		l.mutex.Lock()
		defer l.mutex.Unlock()
		delete(l.subscribers, ch)
	}
}

func (l *Log) Version() int64 {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.version
}

func (l *Log) update(id int64, save bool, change func(task *Task)) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for _, task := range l.tasks {
		if task.ID == id {
			change(task)
			l.changed(save)
			return
		}
	}
}

// changed must be called with the mutex held.
func (l *Log) changed(save bool) {
	l.version++
	for ch := range l.subscribers {
		select {
		case <-ch:
		default:
		}
		ch <- l.version
	}
	if save {
		l.trim()
		l.save()
	}
}

// trim drops the oldest finished tasks beyond the limit, but keeps failed ones.
func (l *Log) trim() {
	kept := []*Task{}
	finished := 0
	for _, task := range l.tasks {
		if task.Status != StatusRunning && task.Status != StatusFailed {
			finished++
			if finished > maxFinished {
				continue
			}
		}
		kept = append(kept, task)
	}
	l.tasks = kept
}

func (l *Log) save() {
	if l.path == "" {
		return
	}
	data, err := json.MarshalIndent(l.tasks, "", " ")
	if err != nil {
		return
	}
	tmp := l.path + ".tmp"
	if os.WriteFile(tmp, data, 0600) == nil {
		os.Rename(tmp, l.path)
	}
}
