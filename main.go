// Task Manager CLI - add, list, done, delete, edit, search, tag, clear with JSON persistence.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"taskmanager/internal/app"
	"taskmanager/internal/store"
)

const usage = `Task Manager - CLI to manage tasks (stored in JSON).

Usage:
  task [options] <command> [arguments]

Commands:
  add [-title "title"] [--due DATE] [--priority low|med|high] [--description "..."] [--tag TAG...]
      (use -title when passing other flags; else: add "title" with no other flags)   Add a task
  list [--all|--done|--pending] [--sort due|priority|created] [--due-today] [--overdue] [--json]
  done <id>                                                            Mark task done
  delete <id> [--force]                                                Delete a task (confirm unless --force)
  edit <id> [--title T] [--due D] [--priority P] [--status S]         Edit a task
  search "keyword"                                                     Search in title/description
  tag add <id> <tag>                                                   Add tag to task
  clear --done                                                         Remove all completed tasks
  reset [-force]                                                       Delete all tasks and start from id 1

Options:
`

func main() {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	defaultData := filepath.Join(home, ".taskmanager", "tasks.json")

	dataFile := flag.String("data", defaultData, "path to tasks JSON file")
	verbose := flag.Bool("verbose", false, "enable debug logging")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *verbose {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	st := store.New(*dataFile)
	defer func() {
		if err := st.Close(); err != nil {
			slog.Error("flush save", "err", err)
		}
	}()

	// Handle Ctrl+C so we flush autosave before exit
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = st.Close()
		os.Exit(0)
	}()

	application := app.New(st)
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	cmdArgs := args[1:]
	slog.Debug("command", "cmd", cmd, "args", cmdArgs, "data", *dataFile)

	var err error
	switch cmd {
	case "add":
		err = runAdd(application, cmdArgs)
	case "list":
		err = runList(application, cmdArgs)
	case "done":
		err = runDone(application, cmdArgs)
	case "delete":
		err = runDelete(application, cmdArgs)
	case "edit":
		err = runEdit(application, cmdArgs)
	case "search":
		err = runSearch(application, cmdArgs)
	case "tag":
		err = runTag(application, cmdArgs)
	case "clear":
		err = runClear(application, cmdArgs)
	case "reset":
		err = runReset(application, cmdArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %q\n", cmd)
		flag.Usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runAdd(a *app.App, args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	titleFlag := fs.String("title", "", "task title (use this when also passing -priority, -due, etc.)")
	due := fs.String("due", "", "due date (YYYY-MM-DD)")
	priority := fs.String("priority", "", "low, med, or high")
	tags := fs.String("tag", "", "comma-separated tags")
	description := fs.String("description", "", "task description (use -description \"...\" for spaces)")
	_ = fs.Parse(args)
	title := strings.TrimSpace(*titleFlag)
	if title == "" {
		title = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}
	if title == "" {
		return fmt.Errorf("task title cannot be empty (use -title \"...\" or put title as first argument)")
	}
	dueDate, err := parseDate(*due)
	if err != nil {
		return err
	}
	var tagList []string
	if *tags != "" {
		for _, s := range strings.Split(*tags, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				tagList = append(tagList, s)
			}
		}
	}

	task, err := a.AddTask(title, *description, *priority, dueDate, tagList)
	if err != nil {
		return err
	}
	fmt.Printf("Added task #%d: %s\n", task.ID, task.Title)
	return nil
}

func runList(a *app.App, args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	done := fs.Bool("done", false, "show only done")
	pending := fs.Bool("pending", false, "show only pending")
	sortBy := fs.String("sort", "created", "due, priority, or created")
	dueToday := fs.Bool("due-today", false, "due today")
	overdue := fs.Bool("overdue", false, "overdue")
	jsonOut := fs.Bool("json", false, "output as JSON")
	_ = fs.Parse(args)

	filter := app.FilterAll
	if *done {
		filter = app.FilterDone
	} else if *pending {
		filter = app.FilterPending
	}
	sortVal := app.SortByCreatedAt
	switch *sortBy {
	case "due":
		sortVal = app.SortByDueDate
	case "priority":
		sortVal = app.SortByPriority
	}

	tasks, err := a.ListTasks(filter, sortVal, *dueToday, *overdue)
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tasks)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks.")
		return nil
	}

	// Table-style output
	fmt.Printf("%-5s %-10s %-8s %-12s %s\n", "ID", "STATUS", "PRIORITY", "DUE", "TITLE")
	fmt.Println(strings.Repeat("-", 60))
	for _, t := range tasks {
		status := t.Status
		pri := t.Priority
		if pri == "" {
			pri = "med"
		}
		due := "-"
		if t.DueDate != nil {
			due = t.DueDate.Format("2006-01-02")
		}
		fmt.Printf("%-5d %-10s %-8s %-12s %s\n", t.ID, status, pri, due, t.Title)
	}
	return nil
}

func runDone(a *app.App, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: task done <id>")
	}
	id, err := parseIntID(args[0])
	if err != nil {
		return err
	}
	if err := a.Done(id); err != nil {
		return err
	}
	fmt.Printf("Marked task #%d as done.\n", id)
	return nil
}

func runDelete(a *app.App, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	force := fs.Bool("force", false, "skip confirmation")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) < 1 {
		return fmt.Errorf("usage: task delete <id> [--force]")
	}
	id, err := parseIntID(rest[0])
	if err != nil {
		return err
	}

	if !*force {
		task, _ := a.GetTask(id)
		if task != nil {
			fmt.Printf("Delete task #%d \"%s\"? [y/N] ", id, task.Title)
			scanner := bufio.NewScanner(os.Stdin)
			if !scanner.Scan() {
				return nil
			}
			if strings.TrimSpace(strings.ToLower(scanner.Text())) != "y" {
				fmt.Println("Cancelled.")
				return nil
			}
		}
	}

	if err := a.Delete(id); err != nil {
		return err
	}
	fmt.Printf("Deleted task #%d.\n", id)
	return nil
}

func runEdit(a *app.App, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: task edit <id> [--title T] [--due D] [--priority P] [--status S]")
	}
	id, err := parseIntID(args[0])
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	title := fs.String("title", "", "new title")
	due := fs.String("due", "", "due date YYYY-MM-DD")
	priority := fs.String("priority", "", "low, med, high")
	status := fs.String("status", "", "todo, in-progress, done")
	_ = fs.Parse(args[1:])

	opts := app.EditOpts{}
	if *title != "" {
		opts.Title = title
	}
	dueDate, err := parseDate(*due)
	if err != nil {
		return err
	}
	if dueDate != nil {
		opts.DueDate = dueDate
	}
	if *priority != "" {
		opts.Priority = priority
	}
	if *status != "" {
		opts.Status = status
	}

	if err := a.Edit(id, opts); err != nil {
		return err
	}
	fmt.Printf("Updated task #%d.\n", id)
	return nil
}

func runSearch(a *app.App, args []string) error {
	keyword := strings.TrimSpace(strings.Join(args, " "))
	if keyword == "" {
		return fmt.Errorf("usage: task search \"keyword\"")
	}
	tasks, err := a.Search(keyword)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("No matching tasks.")
		return nil
	}
	fmt.Printf("%-5s %-10s %s\n", "ID", "STATUS", "TITLE")
	fmt.Println(strings.Repeat("-", 50))
	for _, t := range tasks {
		fmt.Printf("%-5d %-10s %s\n", t.ID, t.Status, t.Title)
	}
	return nil
}

func runTag(a *app.App, args []string) error {
	if len(args) < 3 || strings.ToLower(args[0]) != "add" {
		return fmt.Errorf("usage: task tag add <id> <tag>")
	}
	id, err := parseIntID(args[1])
	if err != nil {
		return err
	}
	tag := strings.Join(args[2:], " ")
	if err := a.TagAdd(id, tag); err != nil {
		return err
	}
	fmt.Printf("Added tag %q to task #%d.\n", tag, id)
	return nil
}

func runClear(a *app.App, args []string) error {
	fs := flag.NewFlagSet("clear", flag.ExitOnError)
	done := fs.Bool("done", false, "remove completed tasks")
	_ = fs.Parse(args)
	if !*done {
		return fmt.Errorf("usage: task clear --done")
	}
	count, err := a.ClearDone()
	if err != nil {
		return err
	}
	fmt.Printf("Removed %d completed task(s).\n", count)
	return nil
}

func runReset(a *app.App, args []string) error {
	fs := flag.NewFlagSet("reset", flag.ExitOnError)
	force := fs.Bool("force", false, "skip confirmation")
	_ = fs.Parse(args)
	if !*force {
		fmt.Print("Delete ALL tasks and reset to empty? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return nil
		}
		if strings.TrimSpace(strings.ToLower(scanner.Text())) != "y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	if err := a.Reset(); err != nil {
		return err
	}
	fmt.Println("All tasks deleted. Next task will be #1.")
	return nil
}

func parseIntID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id < 1 {
		return 0, fmt.Errorf("invalid task ID: %q", s)
	}
	return id, nil
}

func parseDate(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, fmt.Errorf("invalid date (use YYYY-MM-DD): %w", err)
	}
	return &t, nil
}
