# Developer Guide for `fsdriver`

Welcome to the `fsdriver` developer documentation. This document provides a granular, low-level architectural and technical guide for Go engineers who wish to understand, maintain, debug, or extend the codebase.

---

## Table of Contents

- [1. Architectural Overview](#1-architectural-overview)
- [2. Project Layout and Granular File Walkthrough](#2-project-layout-and-granular-file-walkthrough)
  - [2.1 File Tree](#21-file-tree)
  - [2.2 `cmd/fsdriver/main.go`](#22-cmdfsdrivermaingo)
  - [2.3 `internal/engine/filter.go`](#23-internalenginefiltergo)
  - [2.4 `internal/engine/watcher.go`](#24-internalenginewatchergo)
  - [2.5 `internal/server/session.go`](#25-internalserversessiongo)
  - [2.6 `internal/server/handler.go`](#26-internalserverhandlergo)
- [3. Concurrency, Data Flow, and State Management](#3-concurrency-data-flow-and-state-management)
  - [3.1 End-to-End Event Flow](#31-end-to-end-event-flow)
  - [3.2 Mutex Breakdown & Synchronization](#32-mutex-breakdown--synchronization)
  - [3.3 Channels & Backpressure Handling](#33-channels--backpressure-handling)
  - [3.4 Context Propagation & Graceful Teardown](#34-context-propagation--graceful-teardown)
- [4. Build System & Version Injection Mechanics](#4-build-system--version-injection-mechanics)
  - [4.1 Compile-Time Variable Injection (`-ldflags`)](#41-compile-time-variable-injection--ldflags)
  - [4.2 Makefile Targets](#42-makefile-targets)
- [5. Local Development & Debugging Workflow](#5-local-development--debugging-workflow)
  - [5.1 Setup & Dependencies](#51-setup--dependencies)
  - [5.2 Running the Daemon Locally](#52-running-the-daemon-locally)
  - [5.3 Manual Testing with cURL](#53-manual-testing-with-curl)
  - [5.4 Automated Testing with Python Test Client](#54-automated-testing-with-python-test-client)
- [6. Testing Guidelines & How to Write Tests](#6-testing-guidelines--how-to-write-tests)
  - [6.1 Unit Testing `FilterRules`](#61-unit-testing-filterrules)
  - [6.2 Integration Testing `WatcherEngine`](#62-integration-testing-watcherengine)
  - [6.3 HTTP Handler Testing with `httptest`](#63-http-handler-testing-with-httptest)
- [7. Extensibility Recipes (Developing on top of `fsdriver`)](#7-extensibility-recipes-developing-on-top-of-fsdriver)
  - [Recipe 1: Adding a New Command-Line Flag](#recipe-1-adding-a-new-command-line-flag)
  - [Recipe 2: Implementing Recursive Directory Monitoring](#recipe-2-implementing-recursive-directory-monitoring)
  - [Recipe 3: Adding Regular Expression (Regex) Filtering](#recipe-3-adding-regular-expression-regex-filtering)
  - [Recipe 4: Adding a New HTTP Endpoint (e.g., Session Listing)](#recipe-4-adding-a-new-http-endpoint-eg-session-listing)
  - [Recipe 5: Adding a Health Check Endpoint](#recipe-5-adding-a-health-check-endpoint)
- [8. Important Caveats & Best Practices](#8-important-caveats--best-practices)

---

## 1. Architectural Overview

`fsdriver` is inspired by the W3C WebDriver architecture: it decouples client applications from local operating system file tailing mechanisms by exposing a clean HTTP REST and Server-Sent Events (SSE) control surface.

```
+-----------------------------------------------------------------------+
|                            Client (Any Language)                      |
|                  (cURL, Python, Node.js, Go, Browser, etc.)           |
+--------------------+--------------------------------+-----------------+
                     | HTTP REST                      | HTTP SSE Stream
                     | (POST, PUT, DELETE)            | (GET /sessions/{id}/stream)
                     v                                v
+-----------------------------------------------------------------------+
|  `internal/server`: HTTP Server & Session Manager                    |
|                                                                       |
|  - [handler.go] Routes REST API & upgrades connections to SSE         |
|  - [session.go] Tracks active sessions & buffered event channels      |
+------------------------------------+----------------------------------+
                                     | Event Channel (chan LogEvent)
                                     v
+-----------------------------------------------------------------------+
|  `internal/engine`: File System Engine                                |
|                                                                       |
|  - [watcher.go] fsnotify OS watcher + file offset & rotation tracking  |
|  - [filter.go]  In-memory substring include/exclude evaluation engine |
+------------------------------------+----------------------------------+
                                     | OS Kernel Notifications
                                     v
+-----------------------------------------------------------------------+
|  Host OS File System (kqueue / inotify / ReadDirectoryChangesW)       |
+-----------------------------------------------------------------------+
```

### Core Invariants
1. **Dynamic Reconfiguration**: Include and exclude filters can be modified on the fly without closing the client's SSE connection or losing streaming state.
2. **Resilience to Log Rotation**: When target files are rotated or truncated (`newSize < lastOffset`), the driver resets the offset to `0` and resumes reading from the start of the new file.
3. **Non-Blocking Ingestion**: Slow or blocked SSE consumers drop events via non-blocking channel sends rather than deadlocking the OS watcher goroutine.

---

## 2. Project Layout and Granular File Walkthrough

### 2.1 File Tree

```
fsdriver/
├── cmd/
│   └── fsdriver/
│       └── main.go           # Daemon entry point, CLI flags, signal handling
├── internal/
│   ├── engine/
│   │   ├── filter.go         # Dynamic include/exclude substring filtering
│   │   └── watcher.go        # fsnotify watcher, offset tracking, line scanner
│   └── server/
│       ├── handler.go        # HTTP multiplexer, REST endpoints, SSE flusher
│       └── session.go        # Session lifecycle, crypto session ID, registry
├── examples/
│   ├── logs/                 # Working directory for sample log generation
│   └── test_client.py        # Python verification script
├── build/                    # Target directory for compiled binaries
├── DEVELOP.md                # This developer documentation
├── Makefile                  # Build, cross-compilation, and versioning rules
├── README.md                 # User documentation and API reference
├── go.mod                    # Module definition (Go 1.22+ syntax compatibility)
└── go.sum                    # Dependency checksums
```

---

### 2.2 `cmd/fsdriver/main.go`

[`cmd/fsdriver/main.go`](cmd/fsdriver/main.go) serves as the executable entry point.

#### Responsibilities:
1. **Build Metadata Injection**: Defines top-level variables (`Version`, `Commit`, `Date`) populated at link-time via `-ldflags`.
2. **Command-Line Parsing**: Configures `flag.CommandLine` with custom usage info (`flag.Usage`) and registers:
   - `-host`: Bind IP / hostname (`default: "127.0.0.1"`).
   - `-port`: HTTP port (`default: "4444"`).
   - `-version`: Prints version metadata and terminates.
3. **Component Wiring**:
   - Instantiates `server.NewSessionManager()`.
   - Passes the session manager into `server.NewServer(sessionManager)`.
   - Registers routes onto an `http.Server` struct.
4. **Signal Handling & Graceful Shutdown**:
   - Listens on `chan os.Signal` for `os.Interrupt` (`SIGINT`) and `syscall.SIGTERM`.
   - Starts `httpServer.ListenAndServe()` in a separate background goroutine.
   - Upon receiving a termination signal, executes `httpServer.Shutdown(ctx)` with a 10-second timeout to flush open streams before exiting cleanly.

---

### 2.3 `internal/engine/filter.go`

[`internal/engine/filter.go`](internal/engine/filter.go) provides the pure filtering logic executed on each log line before emission.

#### Data Structures:
```go
type FilterRules struct {
    IncludePatterns []string `json:"include_patterns,omitempty"`
    ExcludePatterns []string `json:"exclude_patterns,omitempty"`
}
```

#### Evaluation Rules (`Matches(line string) bool`):
1. **Default State**: If both `IncludePatterns` and `ExcludePatterns` are empty, it returns `true` (unfiltered pass-through).
2. **Include Logic (OR)**: If `IncludePatterns` is populated, the line must contain **at least one** of the patterns (`strings.Contains`). If none match, it returns `false`.
3. **Exclude Logic (AND-NOT)**: If `ExcludePatterns` is populated, the line must **not** contain any of the patterns. If any exclude pattern matches, it returns `false` immediately.

---

### 2.4 `internal/engine/watcher.go`

[`internal/engine/watcher.go`](internal/engine/watcher.go) handles file-system events, line extraction, and event emission.

#### Data Structures:
```go
type LogEvent struct {
    Path      string    `json:"path"`      // Absolute or relative file path
    Line      string    `json:"line"`      // Log line content (trimmed of newline)
    Timestamp time.Time `json:"timestamp"` // Ingestion timestamp
}

type WatcherEngine struct {
    Directories []string
    OutputChan  chan LogEvent

    filterMu sync.RWMutex
    Filters  FilterRules

    offsets map[string]int64  // Keyed by file path: byte offset of last read
    fileMu  sync.Mutex        // Synchronizes file access and offset mutations
}
```

#### Lifecycle & Key Methods:
- `NewWatcherEngine(dirs, filters, outChan)`: Constructor initializing the `offsets` map.
- `UpdateFilters(newFilters)`: Thread-safe update of filter rules guarded by `w.filterMu.Lock()`.
- `Start(ctx context.Context)`:
  - Initializes `fsnotify.NewWatcher()`.
  - Attaches watchers to each directory in `w.Directories`.
  - Executes `w.scanDirectory(dir)` to record initial state.
  - Enters a select loop listening on:
    - `<-ctx.Done()`: Cleans up and exits the goroutine.
    - `<-watcher.Events`: Filters for `fsnotify.Create` and `fsnotify.Write`, invoking `w.processFile(event.Name)`.
    - `<-watcher.Errors`: Logs watcher errors.
- `scanDirectory(dir)`: Reads existing entries in a directory using `os.ReadDir` and runs `processFile` on non-directory entries.
- `processFile(path)`:
  1. Acquires `w.fileMu.Lock()`.
  2. Inspects `os.Stat(path)` to verify the file exists and is not a directory.
  3. Compares `info.Size()` with `w.offsets[path]`:
     - **Rotation / Truncation Check**: If `size < lastOffset`, `lastOffset` is reset to `0`.
     - **No-op Check**: If `size == lastOffset`, returns immediately.
  4. Seeks to `lastOffset` via `file.Seek(lastOffset, io.SeekStart)`.
  5. Scans forward line-by-line using `bufio.Scanner`.
  6. For each line, acquires `w.filterMu.RLock()`, runs `w.Filters.Matches(line)`, and releases lock.
  7. If matched, performs a **non-blocking write** to `w.OutputChan`:
     ```go
     select {
     case w.OutputChan <- LogEvent{...}:
     default:
         // Drop event if consumer is slow and buffer is full
     }
     ```
  8. Records the new byte position (`file.Seek(0, io.SeekCurrent)`) into `w.offsets[path]`.

---

### 2.5 `internal/server/session.go`

[`internal/server/session.go`](internal/server/session.go) isolates client monitoring tasks into managed sessions.

#### Data Structures:
```go
type Session struct {
    ID          string
    Directories []string
    Engine      *engine.WatcherEngine
    OutputChan  chan engine.LogEvent
    Ctx         context.Context
    Cancel      context.CancelFunc
}

type SessionManager struct {
    mu       sync.RWMutex
    sessions map[string]*Session
}
```

#### Lifecycle Methods:
- `CreateSession(directories, initialFilters)`:
  - Generates a cryptographically secure 16-byte random hexadecimal session ID using `crypto/rand`.
  - Creates a cancellable context (`context.WithCancel(context.Background())`).
  - Allocates a buffered event channel of size 1,000 (`make(chan engine.LogEvent, 1000)`).
  - Instantiates `engine.NewWatcherEngine`.
  - Spawns `watcherEngine.Start(ctx)` in a dedicated goroutine.
  - Saves the session under write lock (`sm.mu.Lock()`).
- `GetSession(id)`: Returns the session pointer and presence boolean under read lock (`sm.mu.RLock()`).
- `RemoveSession(id)`: Under write lock (`sm.mu.Lock()`), triggers `session.Cancel()`, closes `session.OutputChan`, and deletes the session key from `sm.sessions`.

---

### 2.6 `internal/server/handler.go`

[`internal/server/handler.go`](internal/server/handler.go) binds RESTful routes and translates HTTP connections to Server-Sent Events.

#### Route Definitions (`RegisterRoutes()`):
Using Go 1.22+ enhanced pattern routing on standard `http.ServeMux`:
```go
mux.HandleFunc("POST /sessions", s.handleCreateSession)
mux.HandleFunc("GET /sessions/", s.handleStreamSession)
mux.HandleFunc("PUT /sessions/", s.handleUpdateFilters)
mux.HandleFunc("DELETE /sessions/", s.handleDeleteSession)
```

#### Handler Implementations:
1. `handleCreateSession`:
   - Decodes JSON payload `{"directories": [...], "include_patterns": [...], "exclude_patterns": [...]}`.
   - Validates that at least one directory is provided.
   - Calls `s.SessionManager.CreateSession(...)` and returns `201 Created` with `session_id` and `stream_url`.
2. `handleStreamSession`:
   - Extracts `sessionID` from `/sessions/{id}/stream`.
   - Validates session existence.
   - Sets required SSE response headers:
     ```http
     Content-Type: text/event-stream
     Cache-Control: no-cache
     Connection: keep-alive
     Access-Control-Allow-Origin: *
     ```
   - Verifies `http.ResponseWriter` implements `http.Flusher`.
   - Emits an initial connection handshake: `event: connected\ndata: {"session_id":"..."}\n\n`.
   - Enters streaming select loop:
     - `<-r.Context().Done()`: Client disconnected; exits gracefully.
     - `<-session.Ctx.Done()`: Session was closed server-side; exits cleanly.
     - `case event, ok := <-session.OutputChan`: Marshals `LogEvent` to JSON, writes formatted `data: <json>\n\n`, and calls `flusher.Flush()`.
3. `handleUpdateFilters`:
   - Extracts `sessionID` from `/sessions/{id}/filters`.
   - Decodes new `engine.FilterRules`.
   - Calls `session.Engine.UpdateFilters(newFilters)` (atomic update).
   - Returns `200 OK` with `{"status": "filters_updated"}`.
4. `handleDeleteSession`:
   - Extracts `sessionID` from `/sessions/{id}`.
   - Calls `s.SessionManager.RemoveSession(sessionID)`.
   - Returns `200 OK` with `{"status": "session_closed"}`.

---

## 3. Concurrency, Data Flow, and State Management

### 3.1 End-to-End Event Flow

```
[Target File Written on Disk]
          |
          v
[OS Kernel Notification] (inotify/kqueue)
          |
          v
[fsnotify.Watcher]
          | (watcher.Events channel)
          v
[WatcherEngine.Start goroutine]
          | calls processFile()
          v
[WatcherEngine.processFile]
    +-- Lock fileMu
    +-- Check file size vs offsets[path]
    +-- Handle rotation if size < offset
    +-- Seek to last offset
    +-- bufio.Scanner loop:
    |       +-- RLock filterMu -> Filters.Matches() -> RUnlock
    |       +-- If matches -> Non-blocking write to OutputChan (buffer: 1000)
    +-- Save new offset in offsets[path]
    +-- Unlock fileMu
          |
          v (OutputChan)
[handleStreamSession goroutine]
          | (reads OutputChan)
          v
[http.Flusher.Flush()]
          |
          v (HTTP Chunked text/event-stream)
[Client (e.g. Browser / curl / test_client.py)]
```

### 3.2 Mutex Breakdown & Synchronization

To avoid data races and deadlocks, `fsdriver` uses three distinct mutexes with strict boundaries:

| Mutex | Type | Scope | Protected Resource | Usage Invariant |
| :--- | :--- | :--- | :--- | :--- |
| `SessionManager.mu` | `sync.RWMutex` | `server/session.go` | `sm.sessions` map | Locked during session creation, lookup, and deletion. Never hold while performing file I/O. |
| `WatcherEngine.filterMu` | `sync.RWMutex` | `engine/watcher.go` | `w.Filters` | Read-locked (`RLock`) during line scanning. Write-locked (`Lock`) during `UpdateFilters()`. Allows zero-downtime updates. |
| `WatcherEngine.fileMu` | `sync.Mutex` | `engine/watcher.go` | `w.offsets` map & file disk operations | Serializes `processFile()` so concurrent write events on the same file don't corrupt read positions. |

> **Lock Ordering Rule**: If you ever need to access both `fileMu` and `filterMu`, always acquire `fileMu` first, then acquire `filterMu`. Currently, locks are never nested except reading `filterMu` inside `processFile` while holding `fileMu`.

### 3.3 Channels & Backpressure Handling

- **Channel Size**: Each session has an `OutputChan` created with buffer capacity `1000`:
  ```go
  outputChan := make(chan engine.LogEvent, 1000)
  ```
- **Drop Policy**: If a client's network connection is slow or stalled, the channel may fill up. In `processFile`:
  ```go
  select {
  case w.OutputChan <- LogEvent{...}:
  default:
      // Buffer full: drop event to protect watcher goroutine
  }
  ```
  *Why this matters:* Without the `default` case, a single stalled client would block `processFile()`, which would freeze `watcher.Events`, starving all files and sessions in the process.

### 3.4 Context Propagation & Graceful Teardown

`fsdriver` manages lifecycle cleanly via Go contexts:

1. **Session Context (`session.Ctx`, `session.Cancel`)**:
   - Cancelled when `DELETE /sessions/{id}` is called or server shuts down.
   - Terminates `watcherEngine.Start()`.
   - Causes `handleStreamSession` to break out of the SSE loop and end the HTTP stream.
2. **Request Context (`r.Context()`)**:
   - If the client drops the SSE connection, `r.Context().Done()` triggers.
   - The streaming loop exits immediately without terminating the session or watcher.
3. **Application Shutdown Context (`cmd/fsdriver/main.go`)**:
   - Intercepts `SIGINT`/`SIGTERM`.
   - `httpServer.Shutdown(ctx)` stops receiving new requests and waits up to 10 seconds for active streams to close.

---

## 4. Build System & Version Injection Mechanics

### 4.1 Compile-Time Variable Injection (`-ldflags`)

`fsdriver` injects versioning metadata directly into compiled binaries without modifying source files or committing version tags to code.

In [`cmd/fsdriver/main.go`](cmd/fsdriver/main.go):
```go
var (
    Version = "dev"
    Commit  = "none"
    Date    = "unknown"
)
```

During build, the [`Makefile`](Makefile) computes these values from the local Git workspace:
```makefile
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS = -ldflags "-s -w -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Date=$(DATE)"
```

- `-s -w`: Strips debugging symbols and DWARF tables to minimize binary size.
- `-X package.Variable=Value`: Replaces string variables in the specified package at compile time.

### 4.2 Makefile Targets

| Target | Description | Example Output |
| :--- | :--- | :--- |
| `make` / `make build` | Builds native executable for host architecture inside `./build/` | `./build/fsdriver` |
| `make all` | Cross-compiles stripped binaries for Linux, macOS, and Windows (`amd64` and `arm64`) | `./build/fsdriver-linux-amd64`, `./build/fsdriver-windows-amd64.exe`, etc. |
| `make version` | Prints computed Git version strings without compiling | `Version: v1.0.0`, `Commit: a1b2c3d`, `Date: ...` |
| `make clean` | Deletes the `./build` directory | Removes all compiled binaries |

---

## 5. Local Development & Debugging Workflow

### 5.1 Setup & Dependencies

Ensure you have Go installed (version 1.22 or newer recommended; repository uses `go 1.26.2` in `go.mod`).

```bash
# Verify Go version
go version

# Download dependencies
go mod download

# Verify dependency checksums
go mod verify
```

### 5.2 Running the Daemon Locally

You can run `fsdriver` directly via `go run` or compile it with `make`:

```bash
# Run directly with custom host and port
go run ./cmd/fsdriver -host 127.0.0.1 -port 4444

# Or build and execute native binary
make build
./build/fsdriver -port 4444
```

### 5.3 Manual Testing with cURL

Open two terminal windows:

**Terminal 1: Setup Log File and Stream**
```bash
# 1. Prepare dummy directory and file
mkdir -p /tmp/fsdriver-test
touch /tmp/fsdriver-test/app.log

# 2. Create session
curl -s -X POST http://127.0.0.1:4444/sessions \
  -H "Content-Type: application/json" \
  -d '{"directories": ["/tmp/fsdriver-test"]}'

# (Copy the session_id from the response JSON)

# 3. Connect to the SSE stream
curl -N http://127.0.0.1:4444/sessions/<SESSION_ID>/stream
```

**Terminal 2: Generate Events and Update Filters**
```bash
# Write logs
echo "INFO: User logged in" >> /tmp/fsdriver-test/app.log
echo "ERROR: Database failed" >> /tmp/fsdriver-test/app.log

# Update session to only receive ERROR logs
curl -X PUT http://127.0.0.1:4444/sessions/<SESSION_ID>/filters \
  -H "Content-Type: application/json" \
  -d '{"include_patterns": ["ERROR"]}'

# Write more logs (only the ERROR will show in Terminal 1)
echo "INFO: Heartbeat" >> /tmp/fsdriver-test/app.log
echo "ERROR: Disk full" >> /tmp/fsdriver-test/app.log

# Clean up session
curl -X DELETE http://127.0.0.1:4444/sessions/<SESSION_ID>
```

### 5.4 Automated Testing with Python Test Client

An end-to-end Python test script is available in `examples/test_client.py`:

```bash
# In one terminal, run the driver
./build/fsdriver -port 4444

# In another terminal, run the python client
python3 examples/test_client.py
```

---

## 6. Testing Guidelines & How to Write Tests

When adding features, tests should be written in standard Go `*_test.go` format. Below are recommended templates and patterns for testing each component.

### 6.1 Unit Testing `FilterRules`

Create `internal/engine/filter_test.go`:

```go
package engine_test

import (
    "fsdriver/internal/engine"
    "testing"
)

func TestFilterRules_Matches(t *testing.T) {
    tests := []struct {
        name    string
        rules   engine.FilterRules
        line    string
        want    bool
    }{
        {
            name:  "Pass through when no rules specified",
            rules: engine.FilterRules{},
            line:  "ANY RANDOM LOG LINE",
            want:  true,
        },
        {
            name: "Include match success",
            rules: engine.FilterRules{
                IncludePatterns: []string{"ERROR", "WARN"},
            },
            line: "2026-11-02 ERROR connection reset",
            want: true,
        },
        {
            name: "Include match failure",
            rules: engine.FilterRules{
                IncludePatterns: []string{"ERROR"},
            },
            line: "2026-11-02 INFO connection established",
            want: false,
        },
        {
            name: "Exclude match discards line",
            rules: engine.FilterRules{
                ExcludePatterns: []string{"healthcheck"},
            },
            line: "GET /healthcheck 200 OK",
            want: false,
        },
        {
            name: "Exclude takes precedence over Include",
            rules: engine.FilterRules{
                IncludePatterns: []string{"ERROR"},
                ExcludePatterns: []string{"IGNORED"},
            },
            line: "ERROR: Something happened but IGNORED",
            want: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := tt.rules.Matches(tt.line)
            if got != tt.want {
                t.Errorf("Matches(%q) = %v, want %v", tt.line, got, tt.want)
            }
        })
    }
}
```

### 6.2 Integration Testing `WatcherEngine`

Create `internal/engine/watcher_test.go` using `t.TempDir()`:

```go
package engine_test

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"

    "fsdriver/internal/engine"
)

func TestWatcherEngine_AppendAndRotate(t *testing.T) {
    tmpDir := t.TempDir()
    logFile := filepath.Join(tmpDir, "test.log")

    if err := os.WriteFile(logFile, []byte("line 1\n"), 0644); err != nil {
        t.Fatalf("failed to create log file: %v", err)
    }

    outChan := make(chan engine.LogEvent, 100)
    w := engine.NewWatcherEngine([]string{tmpDir}, engine.FilterRules{}, outChan)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go func() {
        _ = w.Start(ctx)
    }()

    // Allow watcher initialization
    time.Sleep(100 * time.Millisecond)

    // Append to file
    f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
    if err != nil {
        t.Fatalf("failed to open log file: %v", err)
    }
    _, _ = f.WriteString("line 2 appended\n")
    _ = f.Close()

    select {
    case event := <-outChan:
        if event.Line != "line 2 appended" {
            t.Errorf("unexpected line received: %s", event.Line)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("timed out waiting for log event")
    }
}
```

### 6.3 HTTP Handler Testing with `httptest`

Create `internal/server/handler_test.go`:

```go
package server_test

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "fsdriver/internal/server"
)

func TestCreateSessionHandler(t *testing.T) {
    sm := server.NewSessionManager()
    srv := server.NewServer(sm)
    handler := srv.RegisterRoutes()

    payload := server.CreateSessionRequest{
        Directories: []string{"/tmp"},
    }
    body, _ := json.Marshal(payload)

    req := httptest.NewRequest("POST", "/sessions", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusCreated {
        t.Fatalf("expected status 201, got %d", rec.Code)
    }

    var resp server.CreateSessionResponse
    if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
        t.Fatalf("failed to parse response: %v", err)
    }

    if resp.SessionID == "" || resp.StreamURL == "" {
        t.Errorf("expected session_id and stream_url in response")
    }
}
```

---

## 7. Extensibility Recipes (Developing on top of `fsdriver`)

Here are concrete guides for common enhancements.

### Recipe 1: Adding a New Command-Line Flag

Suppose you want to add a `-buffer-size` flag to configure channel buffer size instead of hardcoding `1000`.

1. **In `cmd/fsdriver/main.go`**:
   ```go
   bufferSizeFlag := flag.Int("buffer-size", 1000, "Channel buffer size for log events per session")
   ```
2. **In `internal/server/session.go`**:
   Add `bufferSize int` to `SessionManager`:
   ```go
   type SessionManager struct {
       mu         sync.RWMutex
       sessions   map[string]*Session
       bufferSize int
   }

   func NewSessionManager(bufferSize int) *SessionManager {
       return &SessionManager{
           sessions:   make(map[string]*Session),
           bufferSize: bufferSize,
       }
   }
   ```
   In `CreateSession`:
   ```go
   outputChan := make(chan engine.LogEvent, sm.bufferSize)
   ```

---

### Recipe 2: Implementing Recursive Directory Monitoring

Currently, `watcher.Add(dir)` only watches the top level of each directory. To watch nested subdirectories dynamically:

In `internal/engine/watcher.go`:
```go
// Add helper to recursively register directories
func (w *WatcherEngine) addRecursive(watcher *fsnotify.Watcher, root string) error {
    return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if d.IsDir() {
            if err := watcher.Add(path); err != nil {
                return err
            }
            w.scanDirectory(path)
        }
        return nil
    })
}
```
And inside `watcher.Events` loop:
```go
if event.Op&fsnotify.Create == fsnotify.Create {
    if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
        _ = w.addRecursive(watcher, event.Name)
    } else {
        w.processFile(event.Name)
    }
}
```

---

### Recipe 3: Adding Regular Expression (Regex) Filtering

To support regex filtering alongside substring matching:

1. In `internal/engine/filter.go`, add compiled regex slices to `FilterRules`:
   ```go
   type FilterRules struct {
       IncludePatterns []string `json:"include_patterns,omitempty"`
       ExcludePatterns []string `json:"exclude_patterns,omitempty"`
       IncludeRegex    []string `json:"include_regex,omitempty"`
       ExcludeRegex    []string `json:"exclude_regex,omitempty"`

       compiledInclude []*regexp.Regexp
       compiledExclude []*regexp.Regexp
   }
   ```
2. Add a `Compile()` method called in `UpdateFilters()` and `Matches()` to evaluate against `re.MatchString(line)`.

---

### Recipe 4: Adding a New HTTP Endpoint (e.g., Session Listing)

To add `GET /sessions` to inspect active sessions:

1. **In `internal/server/session.go`**:
   ```go
   func (sm *SessionManager) ListSessions() []string {
       sm.mu.RLock()
       defer sm.mu.RUnlock()

       ids := make([]string, 0, len(sm.sessions))
       for id := range sm.sessions {
           ids = append(ids, id)
       }
       return ids
   }
   ```
2. **In `internal/server/handler.go`**:
   Register the handler in `RegisterRoutes()`:
   ```go
   mux.HandleFunc("GET /sessions", s.handleListSessions)
   ```
   Implement the handler:
   ```go
   func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
       sessions := s.SessionManager.ListSessions()
       w.Header().Set("Content-Type", "application/json")
       json.NewEncoder(w).Encode(map[string]interface{}{
           "sessions": sessions,
           "count":    len(sessions),
       })
   }
   ```

---

### Recipe 5: Adding a Health Check Endpoint

In `internal/server/handler.go`:
```go
mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
})
```

---

## 8. Important Caveats & Best Practices

1. **Channel Closing Invariant**: Never close `session.OutputChan` from `handleStreamSession`. In Go, channels should only be closed by the owner/sender (`RemoveSession` in `session.go`). Closing a channel with active senders causes a runtime panic.
2. **Scanner Buffer Limits**: `bufio.NewScanner` defaults to a maximum token buffer of 64KB (`bufio.MaxScanTokenSize`). If target log files have lines exceeding 64KB, `scanner.Scan()` will fail with `bufio.ErrTooLong`. For workloads with giant JSON logs, configure `scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)` before scanning.
3. **Legacy Comments**: In `internal/engine/watcher.go`, there are legacy `// FIXME: Add the code for this function` comments (lines 62, 76, 95). Note that `scanDirectory` and `processFile` are already implemented; these comments are harmless artifacts from initial prototyping.
4. **File System Event Coalescing**: On platforms like macOS (`kqueue`), rapid successive file appends may produce coalesced file events. `fsdriver` handles this correctly because `processFile` reads up to EOF (`io.Seek(0, io.SeekCurrent)`) rather than assuming one event equals one line.
