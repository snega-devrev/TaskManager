# Task Manager CLI

A command-line task manager written in Go with JSON persistence, debounced autosave, atomic writes, and backup.

## Features

- **CRUD** – Add, list, done, delete, edit tasks
- **Rich task model** – Title, description, priority (low/med/high), due date, tags, status (todo/in-progress/done)
- **Filters & sort** – List by all/done/pending, due today, overdue; sort by due date, priority, or created
- **Safety** – Delete asks for confirmation unless `--force`; backup file (`tasks.json.bak`) before overwrite; atomic writes
- **Autosave** – Debounced (300ms) background save to reduce disk writes
- **Table & JSON output** – `list --json` for automation
- **Search** – By keyword in title/description
- **Tags** – `tag add <id> <tag>`
- **Clear done** – `clear --done` to remove all completed tasks
- **Signal handling** – Ctrl+C flushes pending save before exit
- **Logging** – `-verbose` for debug (slog)

## Build

```bash
go build -o task .
```

## Usage

```bash
# Add tasks: use -title when passing other flags (due, priority, tag, description)
./task add "Buy groceries"
./task add -title "Finish report" -due 2026-02-20 -priority high -tag work
./task add -title "Project review" -description "Q1 summary" -priority med -due 2026-03-07 -tag work,college

# List (default: all)
./task list
./task list -pending
./task list -done
./task list -due-today -overdue
./task list -sort due
./task list -sort priority
./task list -json

# Mark done
./task done 1

# Delete (confirmation unless -force)
./task delete 2
./task delete 2 -force

# Edit
./task edit 1 -title "New title" -due 2026-02-25 -priority low -status in-progress

# Search
./task search "report"

# Tags
./task tag add 1 college

# Clear completed
./task clear -done

# Global options
./task -data /path/to/tasks.json list
./task -verbose add -title "debug task"
```

Default data file: `~/.taskmanager/tasks.json`. Override with `-data`.

## Project structure

- **`internal/domain`** – Task model (id, title, description, priority, due_date, tags, status, created_at, updated_at, completed_at); validation helpers
- **`internal/store`** – JSON persistence: atomic write (temp file → rename), backup before overwrite, `sync.RWMutex`, debounced autosave goroutine
- **`internal/app`** – Business logic (AddTask, ListTasks, Done, Delete, Edit, Search, TagAdd, ClearDone); no I/O
- **`main.go`** – CLI: subcommands, flags, table/JSON output, confirmation, signal handling

## Practices

- Separation of logic (app) from I/O (main, store)
- Input validation (empty title, invalid date/priority/status, invalid id)
- Wrapped errors (`fmt.Errorf("...: %w", err)`)
- Atomic writes + backup
- Concurrency-safe store (mutex + single autosave worker)
- Context/signal handling for graceful shutdown
- Tests for store and app
- Standard library + `log/slog` only
