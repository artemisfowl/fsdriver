# fsdriver

A cross-platform, language-agnostic file monitoring driver for streaming file system events and log data over HTTP and Server-Sent Events (SSE).

Just as WebDriver provides a standardized HTTP interface to control web browsers remotely, `fsdriver` provides a standardized HTTP/SSE interface to orchestrate, monitor, and stream file system activity across multiple directories in real-time.

---

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
  - [System Overview](#system-overview)
  - [Core Components](#core-components)
- [API Endpoints Reference](#api-endpoints-reference)
  - [1. Create Session (`POST /sessions`)](#1-create-session-post-sessions)
  - [2. Stream Log Events (`GET /sessions/:id/stream`)](#2-stream-log-events-get-sessionsidstream)
  - [3. Update Filters (`PUT /sessions/:id/filters`)](#3-update-filters-put-sessionsidfilters)
  - [4. Terminate Session (`DELETE /sessions/:id`)](#4-terminate-session-delete-sessionsid)
- [Dynamic Filtering Logic](#dynamic-filtering-logic)
  - [Filter Evaluation Pipeline](#filter-evaluation-pipeline)
  - [Matching Rules](#matching-rules)
  - [On-the-Fly Runtime Updates](#on-the-fly-runtime-updates)
- [Quick Start](#quick-start)
- [Building and Versioning](#building-and-versioning-fsdriver)
  - [Build Commands](#build-commands)
  - [Checking Version Information](#checking-the-version-of-a-compileddownloaded-binary)
  - [Checking Help Information](#checking-help-information)
  - [Tagging a New Release](#tagging-a-new-release)
- [Frequently Asked Questions (FAQ)](#frequently-asked-questions-faq)
- [License & Author](#license--author)

---

## Features

- **Language-Agnostic & Remote**: Consume real-time log streams from any language or runtime (Python, Node.js, Go, Rust, cURL, web browsers) via standard HTTP and Server-Sent Events (SSE).
- **Dynamic Real-Time Filtering**: Modify include/exclude filter rules on running streams without restarting sessions or dropping connections.
- **Cross-Platform OS Notifications**: Leverages native kernel-level file notification mechanisms (`inotify` on Linux, `kqueue` on macOS, `ReadDirectoryChangesW` on Windows) via `fsnotify`.
- **Log Rotation Resilient**: Automatically detects file truncations and log rotations, seamlessly resetting file offsets without losing track of active logs.
- **Session Isolation**: Manage independent, concurrent tailing sessions across arbitrary directories.
- **Zero Heavy Dependencies**: Built with Go for lightweight memory footprints and static cross-compilation.

---

## Architecture

### System Overview

`fsdriver` decouples file ingestion from client consumption using a session-based HTTP/SSE model:

1. **Client Interaction**: Clients written in any language (Python, Node.js, Go, Rust, browser scripts, cURL) interact with the driver via standard HTTP REST endpoints to create, configure, update, and terminate monitoring sessions.
2. **Session Lifecycle**: The `SessionManager` allocates an isolated session identified by a cryptographically secure 128-bit ID, complete with a buffered `LogEvent` channel and a dedicated cancellation context.
3. **File System Watching**: The `WatcherEngine` attaches OS-level file notification hooks (`fsnotify`) to all requested directories, tracking byte offsets per file and handling log rotations automatically.
4. **Line-by-Line Filtering**: Newly appended lines are incrementally read and passed through the dynamic filter engine (protected by a thread-safe `sync.RWMutex`).
5. **Real-Time Streaming**: Surviving events are dispatched across long-lived Server-Sent Events (SSE) HTTP connections (`text/event-stream`) to connected subscribers.

### Core Components

1. **HTTP Server & Router ([`internal/server/handler.go`](file:///Users/oldgod/data/code/test_bed/skunkworks/ned/go/fsdriver/internal/server/handler.go))**:
   - Implements standard REST endpoints to create, update, and terminate sessions.
   - Provides a Server-Sent Events (SSE) streaming endpoint using Go's `http.Flusher` to push structured JSON records (`LogEvent`) to connected clients as new lines are written.

2. **Session Manager ([`internal/server/session.go`](file:///Users/oldgod/data/code/test_bed/skunkworks/ned/go/fsdriver/internal/server/session.go))**:
   - Manages independent, concurrent monitoring sessions mapped by cryptographically secure 128-bit random hexadecimal IDs.
   - Handles thread-safe session registration, resource cleanup, buffered channels, and context cancellation to prevent goroutine leaks.

3. **Watcher Engine ([`internal/engine/watcher.go`](file:///Users/oldgod/data/code/test_bed/skunkworks/ned/go/fsdriver/internal/engine/watcher.go))**:
   - **FS Event Monitoring**: Subscribes to directory create/write events using [`fsnotify`](https://github.com/fsnotify/fsnotify).
   - **Offset & Rotation Tracking**: Tracks file read positions (`offsets map[string]int64`). If a file size drops below the last recorded offset (log rotation or truncation), the offset automatically resets to 0.
   - **Line Scanner**: Incremental reads using `bufio.Scanner` ensure that only newly appended lines are read from disk.

4. **Dynamic Filter Rules ([`internal/engine/filter.go`](file:///Users/oldgod/data/code/test_bed/skunkworks/ned/go/fsdriver/internal/engine/filter.go))**:
   - Encapsulates include and exclude substring filtering logic.
   - Thread-safe updates guarded by a read-write mutex (`sync.RWMutex`) allow filters to be swapped during active streaming without missing events or corrupting memory.

---

## API Endpoints Reference

| Method | Path | Description |
| :--- | :--- | :--- |
| `POST` | `/sessions` | Creates a new watcher session for specified directories. Returns `session_id` and `stream_url`. |
| `GET` | `/sessions/{id}/stream` | Upgrades connection to SSE and streams live log events as JSON payloads. |
| `PUT` | `/sessions/{id}/filters` | Dynamically updates `include_patterns` and `exclude_patterns` for an active session. |
| `DELETE` | `/sessions/{id}` | Terminates a session, closes the associated file watcher, and frees system resources. |

---

### 1. Create Session (`POST /sessions`)

Creates an isolated monitoring session for one or more directories with optional initial substring filters.

**Request Headers:**
- `Content-Type: application/json`

**Request Body Parameters:**

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `directories` | `string[]` | **Yes** | Array of directory paths to watch (absolute or relative). |
| `include_patterns` | `string[]` | No | Substring filters. If set, only lines containing at least one pattern are streamed. |
| `exclude_patterns` | `string[]` | No | Substring filters. Lines containing any of these patterns are discarded. |

**Example Request:**
```bash
curl -X POST http://localhost:4444/sessions \
  -H "Content-Type: application/json" \
  -d '{
    "directories": ["/var/log/myapp"],
    "include_patterns": ["ERROR", "WARN"],
    "exclude_patterns": ["healthcheck"]
  }'
```

**Response (`201 Created`):**
```json
{
  "session_id": "9f257a6e1d2345bc8890abcdef123456",
  "stream_url": "/sessions/9f257a6e1d2345bc8890abcdef123456/stream"
}
```

**Error Responses:**
- `400 Bad Request`: `{"error": "invalid request body"}` or `{"error": "at least one directory must be specified"}`
- `500 Internal Server Error`: `{"error": "<failure reason>"}`

---

### 2. Stream Log Events (`GET /sessions/{id}/stream`)

Establishes a long-lived Server-Sent Events (SSE) connection. As target files are modified or appended to, matching lines are pushed immediately to the client as JSON payloads.

**Path Parameters:**

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | `string` | **Yes** | The 128-bit hexadecimal session identifier returned by `POST /sessions`. |

**Response Headers:**
- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- `Connection: keep-alive`
- `Access-Control-Allow-Origin: *`

**Example Request:**
```bash
curl -N http://localhost:4444/sessions/9f257a6e1d2345bc8890abcdef123456/stream
```

**SSE Events Delivered:**

1. **Connection Confirmation Event** (`event: connected`):
   Sent immediately upon establishing the SSE connection.
   ```text
   event: connected
   data: {"session_id":"9f257a6e1d2345bc8890abcdef123456"}
   ```

2. **Log Stream Event** (default message event):
   Pushed in real-time as new matching lines are appended to watched files.
   ```text
   data: {"path":"/var/log/myapp/app.log","line":"[12:00:05] ERROR: Database connection timeout","timestamp":"2026-10-25T02:45:00.123456Z"}
   ```

**`LogEvent` Schema:**

| Field | Type | Description |
| :--- | :--- | :--- |
| `path` | `string` | Absolute or relative path to the modified file. |
| `line` | `string` | The appended log line text. |
| `timestamp` | `string` | ISO 8601 UTC timestamp recorded when the line was ingested. |

**Error Responses:**
- `404 Not Found`: `{"error": "session not found"}`

---

### 3. Update Filters (`PUT /sessions/{id}/filters`)

Dynamically reconfigures include/exclude filter rules on an active session. The changes take effect immediately on subsequent file reads without interrupting or dropping the open SSE stream.

**Path Parameters:**

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | `string` | **Yes** | The session identifier. |

**Request Headers:**
- `Content-Type: application/json`

**Request Body Parameters:**

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `include_patterns` | `string[]` | No | New array of include substrings (pass `[]` to remove include constraints). |
| `exclude_patterns` | `string[]` | No | New array of exclude substrings (pass `[]` to remove exclude constraints). |

**Example Request:**
```bash
curl -X PUT http://localhost:4444/sessions/9f257a6e1d2345bc8890abcdef123456/filters \
  -H "Content-Type: application/json" \
  -d '{
    "include_patterns": ["CRITICAL", "PANIC"],
    "exclude_patterns": []
  }'
```

**Response (`200 OK`):**
```json
{
  "status": "filters_updated"
}
```

**Error Responses:**
- `400 Bad Request`: `{"error": "invalid filter rules payload"}`
- `404 Not Found`: `{"error": "session not found"}`

---

### 4. Terminate Session (`DELETE /sessions/{id}`)

Stops file watching goroutines, closes associated channels, and deletes the session from memory. Any open SSE stream connections for this session are terminated gracefully.

**Path Parameters:**

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `id` | `string` | **Yes** | The session identifier. |

**Example Request:**
```bash
curl -X DELETE http://localhost:4444/sessions/9f257a6e1d2345bc8890abcdef123456
```

**Response (`200 OK`):**
```json
{
  "status": "session_closed"
}
```

**Error Responses:**
- `404 Not Found`: `{"error": "session not found"}`

---

## Dynamic Filtering Logic

`fsdriver` allows clients to filter log lines in memory before streaming them over the network. Filters can be configured at session creation and updated at any point during active streaming.

### Filter Evaluation Pipeline

When a file modification event occurs, lines appended since the last file offset are processed sequentially through an in-memory evaluation pipeline:

1. **Include Filter Check**:
   - If no `include_patterns` are specified, the line automatically passes to the next check.
   - If `include_patterns` are specified, the line must contain **at least one** include pattern (*Logical OR*). Lines that do not match are discarded immediately.
2. **Exclude Filter Check**:
   - If no `exclude_patterns` are specified, the line passes.
   - If `exclude_patterns` are specified, the line must **not** contain any exclude pattern (*Logical NOT*). Any matching line is discarded immediately.
3. **Emit to SSE Stream**:
   - Lines that pass both checks are wrapped into a structured `LogEvent` (`path`, `line`, `timestamp`) and sent to the session's SSE output channel.

### Matching Rules

The filter engine evaluates each line using the following rules:

1. **Default Pass-Through**: If both `include_patterns` and `exclude_patterns` are empty, **all** lines pass through.
2. **Include Filter (OR Logic)**: If `include_patterns` contains one or more patterns, a line must match **at least one** pattern. If it matches none, it is discarded immediately.
3. **Exclude Filter (AND-NOT Logic)**: If `exclude_patterns` contains patterns, any line that contains **any** of the exclude patterns is discarded, even if it previously matched an include pattern.
4. **Case Sensitivity**: Pattern checks use case-sensitive substring matching (`strings.Contains`).

### On-the-Fly Runtime Updates

Unlike traditional tools where filter changes require killing and restarting processes, `fsdriver` supports dynamic updates over HTTP:

- **Non-blocking read locks**: During log processing, the watcher acquires a read lock (`w.filterMu.RLock()`) to evaluate lines against current filters.
- **Write lock for atomic replacement**: When a client calls `PUT /sessions/{id}/filters`, the engine acquires an exclusive write lock (`w.filterMu.Lock()`) and swaps the filter rules atomically.
- **Zero Stream Downtime**: The SSE connection remains open and active; events matching the new criteria flow immediately into the established stream.

---

## Quick Start

### 1. Start the `fsdriver` server

```bash
./build/fsdriver -port 4444
```

### 2. Create a session using `curl`

```bash
curl -X POST http://localhost:4444/sessions \
  -H "Content-Type: application/json" \
  -d '{"directories": ["./examples/logs"]}'
```

### 3. Subscribe to the SSE stream

```bash
curl -N http://localhost:4444/sessions/<SESSION_ID>/stream
```

### 4. Dynamically filter for errors only

```bash
curl -X PUT http://localhost:4444/sessions/<SESSION_ID>/filters \
  -H "Content-Type: application/json" \
  -d '{"include_patterns": ["ERROR"]}'
```

A complete Python example demonstrating live streaming, dynamic filter switching, and cleanup is available in [`examples/test_client.py`](file:///Users/oldgod/data/code/test_bed/skunkworks/ned/go/fsdriver/examples/test_client.py).

---

## Building and versioning `fsdriver`

`fsdriver` utilizes Git metadata combined with Go linker flags (`-ldflags`) to inject
versioning information directly into the binary at compile time. This ensures that
every binary built automatically carries its tag, commit hash, and build timestamp
without needing manual version bumps in code.

### Build commands

| Command | Description |
| :--- | :--- |
| `make` / `make build` | Builds the native binary for your host OS inside `./build/fsdriver` |
| `make all` | Cross-compiles binaries for Linux, macOS, and Windows (`amd64` & `arm64`) |
| `make version` | Prints the current Git version strings without building |
| `make clean` | Removes all compiled artifacts in the `./build` directory |

### Checking the version of a compiled/downloaded binary

To verify the version information embedded inside any compiled/downloaded executable, run the binary
with the `--version` flag:

```bash
./build/fsdriver --version
fsdriver version v1.0.0 (commit: ef60be1, built at: 2026-10-18T07:51:00Z)
```

### Checking help information

To check the help information embedded inside the binary, run the binary with the `--help` flag:

```bash
./build/fsdriver --help
fsdriver - A cross-platform, language-agnostic file monitoring driver

Author:  Sayantan Bhattacharya
License: GNU General Public License v3.0 (GPLv3)

Usage of ./fsdriver:
  -port string
        Port for the fsdriver HTTP server (default "4444")
  -version
        Print version information and exit
```

### Tagging a new release

To release a new version, tag your commit in Git and run `make all`:

```bash
git tag -a v1.0.0 -m "Release v1.0.0"
make all
```

All generated binaries in `./build/` will automatically reflect the specified tag (in the aforementioned
example, `v1.0.0`).

---

## Frequently Asked Questions (FAQ)

### What is `fsdriver` and why was it created?
`fsdriver` is inspired by the WebDriver architecture used for browser automation. Traditional log-tailing tools (such as `tail -f`) are coupled to local shells and platform-dependent utilities. `fsdriver` decouples the file watching and tailing engine from the consumer by exposing a standard, language-agnostic HTTP/SSE interface. Any service, script, web dashboard, or test runner can orchestrate file monitoring across any OS without native agent installations or SSH wrappers.

### How does dynamic filtering work without reconnecting?
Each session's `WatcherEngine` maintains its `FilterRules` behind a thread-safe read-write mutex (`sync.RWMutex`). When you issue a `PUT /sessions/{id}/filters` request, the server updates the rule set in memory. The SSE connection remains undisturbed, and subsequent file reads immediately reflect the updated include/exclude rules.

### How does `fsdriver` handle log rotation?
`fsdriver` tracks the byte offset of each watched file. During file modification checks, if the current file size is detected to be smaller than the recorded offset (`size < lastOffset`), `fsdriver` detects that the file has been truncated or rotated, resets the offset to 0, and resumes tailing from the beginning of the new file.

### How are directories scanned upon session initialization?
When a session is created, `fsdriver` adds the target directories to the OS watcher and performs an initial directory scan (`scanDirectory`). File offsets are established so that subsequent writes trigger line-by-line processing.

### What happens when an SSE client disconnects?
When a client closes the HTTP connection to `GET /sessions/{id}/stream`, the HTTP request context (`r.Context().Done()`) cancels, terminating the streaming loop for that request. The session itself remains alive in the session manager until explicitly removed via `DELETE /sessions/{id}` or when the server shuts down.

### Is `fsdriver` truly language-agnostic?
Yes. Since communication relies entirely on standard HTTP REST endpoints and Server-Sent Events (`text/event-stream`), any programming language capable of making HTTP requests and reading chunked streams (e.g., Python `requests`, Node.js `fetch`/`EventSource`, Go `net/http`, cURL) can control and consume streams from `fsdriver`.

### Can `fsdriver` watch multiple directories in a single session?
Yes. The `POST /sessions` endpoint accepts an array of directory paths in the `directories` field. All files within those directories will be monitored, and incoming events will indicate the specific originating file in the `path` field of the `LogEvent`.

### How does graceful shutdown work?
`fsdriver` listens for OS termination signals (`SIGINT`, `SIGTERM`). Upon receiving a signal, it initiates an orderly HTTP server shutdown with a 10-second grace period (`context.WithTimeout`), allowing active streams to flush and close properly.

---

## License & Author

- **Author**: Sayantan Bhattacharya (<skb655952@gmail.com>)
- **License**: GNU General Public License v3.0 (GPLv3)

