package engine

import (
	"bufio"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type LogEvent struct {
	Path      string    `json:"path"`
	Line      string    `json:"line"`
	Timestamp time.Time `json:"timestamp"`
}

type WatcherEngine struct {
	Directories []string
	OutputChan  chan LogEvent

	filterMu sync.RWMutex
	Filters  FilterRules

	offsets map[string]int64
	fileMu  sync.Mutex
}

func (w *WatcherEngine) UpdateFilters(newFilters FilterRules) {
	w.filterMu.Lock()
	defer w.filterMu.Unlock()

	w.Filters = newFilters
}

func NewWatcherEngine(dirs []string, filters FilterRules, outChan chan LogEvent) *WatcherEngine {
	return &WatcherEngine{
		Directories: dirs,
		Filters:     filters,
		OutputChan:  outChan,
		offsets:     make(map[string]int64),
	}
}

func (w *WatcherEngine) Start(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	defer watcher.Close()

	// add directories to fsnotify and perform initial scan of files
	for _, dir := range w.Directories {
		if err := watcher.Add(dir); err != nil {
			return err
		}
		// FIXME: Add the code for this function
		w.scanDirectory(dir)
	}

	// Event loop for checking real-time file updates
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write {
				// FIXME: Add the code for this function as well
				w.processFile(event.Name)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("Watcher error: %s", err)
		}
	}
}

func (w *WatcherEngine) scanDirectory(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			// FIXME: Add the code for this function as mentioned above
			w.processFile(filepath.Join(dir, entry.Name()))
		}
	}
}

func (w *WatcherEngine) processFile(path string) {
	// w.mu.Lock()
	// defer w.mu.Unlock()
	w.fileMu.Lock()
	defer w.fileMu.Unlock()

	// Skip directories
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return
	}

	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return
	}

	size := info.Size()
	lastOffset := w.offsets[path]

	// Handle log rotation
	if size < lastOffset {
		lastOffset = 0
	}

	// If no file change in size, no nedw data added - return the control
	if size == lastOffset {
		return
	}

	// Seek to the last and read offset
	_, err = file.Seek(lastOffset, io.SeekStart)
	if err != nil {
		return
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		w.filterMu.RLock()
		matches := w.Filters.Matches(line)
		w.filterMu.RUnlock()

		// if w.Filters.Matches(line) {
		if matches {
            select {
                case w.OutputChan <- LogEvent{
                    Path:      path,
                    Line:      line,
                    Timestamp: time.Now(),
                }
            }
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Scanner error on %s: %v", path, err)
	}

	// Save the file offset
	newOffset, _ := file.Seek(0, io.SeekCurrent)
	w.offsets[path] = newOffset
}
