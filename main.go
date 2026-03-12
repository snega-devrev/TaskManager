// Task Manager CLI - add, list, done, delete, edit, search, tag with MongoDB persistence.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
	"unicode"

	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"taskmanager/api/proto"
	"taskmanager/internal/app"
	grpcserver "taskmanager/internal/server/grpc"
	"taskmanager/internal/domain"
	"taskmanager/internal/store"
)

// checkConnection verifies the store (MongoDB) is reachable. Call at the start of each interactive operation.
func checkConnection(a *app.App) error {
	_, err := a.ListTasks(app.FilterAll, app.SortByCreatedAt, false, false)
	return err
}

// connectionErrMsg returns a user-friendly message for MongoDB connection errors.
func connectionErrMsg(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "connection refused"),
		strings.Contains(s, "connection closed"),
		strings.Contains(s, "context deadline exceeded"),
		strings.Contains(s, "server selection error"),
		strings.Contains(s, "connection reset"):
		return "MongoDB connection lost. Please check that MongoDB is running and -mongo-uri is correct."
	}
	return err.Error()
}

const usage = `Task Manager - CLI to manage tasks (MongoDB).

Usage:
  task [options]                    Interactive mode (menu-driven; no command = start here)
  task [options] <command> [args]   Command mode (e.g. task add -title "Buy milk")

Commands:
  add [-title "title"] [--due DATE] [--priority low|med|high] [--description "..."] [--tag TAG...]
      (use -title when passing other flags; else: add "title" with no other flags)   Add a task
  list [--all|--done|--pending] [--sort due|priority|created] [--due-today] [--overdue] [--json]
  done <id>                                                            Mark task done
  delete <id> [--force]                                                Delete a task (confirm unless --force)
  edit <id> [--title T] [--due D] [--priority P] [--status S]         Edit a task
  search "keyword"                                                     Search in title/description
  tag add <id> <tag>                                                   Add tag to task
  reset [-force]                                                       Delete all tasks and start from id 1

Options:
  -grpc-addr ADDR   start gRPC server on ADDR (e.g. :50051); can be used with interactive mode
  -user ID          use this user's task list in CLI (same as gRPC user after login); omit for default list
`

func main() {
	mongoURI := flag.String("mongo-uri", os.Getenv("MONGO_URI"), "MongoDB connection URI (e.g. mongodb://localhost:27017); required (or set MONGO_URI)")
	mongoDB := flag.String("mongo-db", "taskmanager", "MongoDB database name")
	mongoColl := flag.String("mongo-coll", "tasks", "MongoDB collection name")
	grpcAddr := flag.String("grpc-addr", "", "if set, start gRPC server on this address (e.g. :50051)")
	jwtSecret := flag.String("jwt-secret", os.Getenv("JWT_SECRET"), "HMAC secret to validate JWT; if set, user_id is read from token 'sub' claim (no manual user-id needed)")
	user := flag.String("user", "", "CLI only: use this user's task list (same data as gRPC when logged in as this user); empty = default list")
	verbose := flag.Bool("verbose", false, "enable debug logging")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *verbose {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	if *mongoURI == "" {
		fmt.Fprintln(os.Stderr, "Error: -mongo-uri is required (or set MONGO_URI environment variable)")
		flag.Usage()
		os.Exit(1)
	}

	args := flag.Args()

	ms, err := store.NewMongo(*mongoURI, *mongoDB, *mongoColl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "MongoDB: %v\n", err)
		os.Exit(1)
	}
	st := store.TaskStore(ms)
	if *user != "" {
		st = store.NewUserScopedStore(ms, strings.TrimSpace(*user))
		slog.Debug("CLI using user-scoped store", "user", *user)
	}
	slog.Debug("using MongoDB store", "db", *mongoDB, "coll", *mongoColl)
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

	if *grpcAddr != "" {
		addr := *grpcAddr
		// Listen on all interfaces so other machines can connect (e.g. :50051 -> 0.0.0.0:50051)
		if addr == "" || addr[0] == ':' {
			addr = "0.0.0.0" + addr
		}
		lis, err := net.Listen("tcp", addr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gRPC listen: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "gRPC server listening on %s (accepting connections from other machines)\n", lis.Addr().String())
		srv := grpc.NewServer(grpc.UnaryInterceptor(grpcserver.JWTUnaryInterceptor(*jwtSecret)))
		grpcSrv := grpcserver.NewServer(ms, *jwtSecret)
		proto.RegisterAuthServiceServer(srv, grpcSrv)
		proto.RegisterTaskServiceServer(srv, grpcSrv)
		reflection.Register(srv)
		go func() {
			slog.Debug("gRPC server listening", "addr", addr)
			if err := srv.Serve(lis); err != nil {
				slog.Error("gRPC server", "err", err)
			}
		}()
	}

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "MongoDB: database %q, collection %q (refresh this in Compass to see changes)\n", *mongoDB, *mongoColl)
		if *user != "" {
			fmt.Fprintf(os.Stderr, "Using task list for user: %s (same as gRPC when logged in as this user)\n", *user)
		}
		runInteractive(application)
		return
	}

	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	cmdArgs := args[1:]
	slog.Debug("command", "cmd", cmd, "args", cmdArgs)

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
	case "reset":
		err = runReset(application, cmdArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %q\n", cmd)
		flag.Usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", connectionErrMsg(err))
		os.Exit(1)
	}
}

const interactiveMenu = `
  1. Add task      2. List tasks    3. Update status    4. Delete task
  5. Edit task     6. Search        7. Tag add      8. Reset all
  0. Exit
`

func runInteractive(a *app.App) {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println()
		fmt.Println(strings.Repeat("->", 50))
		
		fmt.Print(interactiveMenu)
		fmt.Print("Choice: ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		choice := strings.ToLower(parts[0])
		rest := strings.TrimSpace(strings.Join(parts[1:], " "))

		var err error
		switch choice {
		case "0", "exit", "q", "quit":
			fmt.Println("Bye.")
			return
		case "1", "add":
			if err = checkConnection(a); err != nil {
				break
			}
			rest = strings.TrimSpace(rest)
			for {
				if rest == "" {
					fmt.Print("title (required): ")
					if !scanner.Scan() {
						break
					}
					rest = strings.TrimSpace(scanner.Text())
				}
				if rest == "" {
					fmt.Println("Title is required. Please enter a title.")
					rest = ""
					continue
				}
				if isOnlyDigits(rest) {
					fmt.Println("Task title cannot be only numbers. Please enter a valid title.")
					rest = ""
					continue
				}
				if containsNoLetter(rest) {
					fmt.Println("Task title must contain at least one letter. Please enter a valid title.")
					rest = ""
					continue
				}
				break
			}
			addArgs := []string{"-title", rest}
			fmt.Print("description (Enter to skip): ")
			if scanner.Scan() {
				desc := strings.TrimSpace(scanner.Text())
				if desc != "" {
					addArgs = append(addArgs, "-description", desc)
				}
			}
			for {
				fmt.Print("priority (low|med|high, Enter for med): ")
				if !scanner.Scan() {
					break
				}
				p := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if p == "" {
					break
				}
				if domain.ValidPriority(p) {
					addArgs = append(addArgs, "-priority", p)
					break
				}
				fmt.Println("Invalid priority. Use low, med, or high (or Enter for med).")
			}
			fmt.Print("tags (comma-separated, Enter to skip): ")
			if scanner.Scan() {
				tagStr := strings.TrimSpace(scanner.Text())
				if tagStr != "" {
					addArgs = append(addArgs, "-tag", tagStr)
				}
			}
			for {
				fmt.Print("due_date (YYYY-MM-DD, Enter to skip): ")
				if !scanner.Scan() {
					break
				}
				d := strings.TrimSpace(scanner.Text())
				if d == "" {
					break
				}
				if _, dueErr := parseDueDate(d); dueErr != nil {
					fmt.Println(dueErr.Error())
					continue
				}
				addArgs = append(addArgs, "-due", d)
				break
			}
			err = runAdd(a, addArgs)
			if err == nil {
				err = a.FlushSave()
			}
		case "2", "list":
			if err = checkConnection(a); err != nil {
				break
			}
			err = runList(a, nil)
		case "3", "done":
			if err = checkConnection(a); err != nil {
				break
			}
			idStr := rest
			if idStr == "" {
				if !showTasksBeforePrompt(a) {
					err = fmt.Errorf("no tasks")
					break
				}
				fmt.Print("id: ")
				if scanner.Scan() {
					idStr = strings.TrimSpace(scanner.Text())
				}
			}
			if idStr == "" {
				err = fmt.Errorf("id required")
				break
			}
			fmt.Print("1. in progress  2. done — choice: ")
			if !scanner.Scan() {
				break
			}
			statusChoice := strings.TrimSpace(scanner.Text())
			var newStatus string
			switch statusChoice {
			case "1":
				newStatus = domain.StatusInProgress
			case "2":
				newStatus = domain.StatusDone
			default:
				err = fmt.Errorf("choose 1 (in progress) or 2 (done)")
				break
			}
			if err != nil {
				break
			}
			id, parseErr := parseIntID(idStr)
			if parseErr != nil {
				err = parseErr
				break
			}
			task, getErr := a.GetTask(id)
			if getErr != nil {
				err = getErr
				break
			}
			if task == nil {
				err = fmt.Errorf("task #%d not found", id)
				break
			}
			if task.Status == newStatus {
				fmt.Printf("Task #%d is already %s. No change.\n", id, newStatus)
				break
			}
			err = runEdit(a, []string{idStr, "-status", newStatus})
			if err == nil {
				err = a.FlushSave()
			}
		case "4", "delete":
			if err = checkConnection(a); err != nil {
				break
			}
			idStr := rest
			if idStr == "" {
				if !showTasksBeforePrompt(a) {
					err = fmt.Errorf("no tasks to delete")
					break
				}
				fmt.Print("id: ")
				if scanner.Scan() {
					idStr = strings.TrimSpace(scanner.Text())
				}
			}
			if idStr != "" {
				err = runDelete(a, []string{idStr})
				if err == nil {
					err = a.FlushSave()
				}
			} else {
				err = fmt.Errorf("id required")
			}
		case "5", "edit":
			if err = checkConnection(a); err != nil {
				break
			}
			idStr := rest
			if idStr == "" {
				if !showTasksBeforePrompt(a) {
					err = fmt.Errorf("no tasks")
					break
				}
				fmt.Print("id: ")
				if scanner.Scan() {
					idStr = strings.TrimSpace(scanner.Text())
				}
			}
			if idStr == "" {
				err = fmt.Errorf("id required")
				break
			}
			id, parseErr := parseIntID(idStr)
			if parseErr != nil {
				err = parseErr
				break
			}
			task, getErr := a.GetTask(id)
			if getErr != nil {
				err = getErr
				break
			}
			if task == nil {
				err = fmt.Errorf("task #%d not found", id)
				break
			}
			editArgs := []string{idStr}
			fmt.Print("title (Enter to keep): ")
			if scanner.Scan() {
				rawTitle := scanner.Text()
				t := strings.TrimSpace(rawTitle)
				if rawTitle != "" {
					if t == "" {
						err = fmt.Errorf("title cannot be empty")
						break
					}
					if isOnlyDigits(t) {
						err = fmt.Errorf("title cannot be only numbers")
						break
					}
					if containsNoLetter(t) {
						err = fmt.Errorf("title must contain at least one letter")
						break
					}
					editArgs = append(editArgs, "-title", t)
				}
			}
			if err != nil {
				break
			}
			fmt.Print("due_date (YYYY-MM-DD, Enter to keep): ")
			if scanner.Scan() {
				d := strings.TrimSpace(scanner.Text())
				if d != "" {
					if _, dueErr := parseDueDate(d); dueErr != nil {
						err = dueErr
						break
					}
					editArgs = append(editArgs, "-due", d)
				}
			}
			if err != nil {
				break
			}
			fmt.Print("priority (low|med|high, Enter to keep): ")
			if scanner.Scan() {
				p := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if p != "" {
					if !domain.ValidPriority(p) {
						err = fmt.Errorf("invalid priority: use low, med, or high")
						break
					}
					editArgs = append(editArgs, "-priority", p)
				}
			}
			if err != nil {
				break
			}
			fmt.Print("status (todo|in-progress|done, Enter to keep): ")
			if scanner.Scan() {
				s := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if s != "" {
					if !domain.ValidStatus(s) {
						err = fmt.Errorf("invalid status: use todo, in-progress, or done")
						break
					}
					editArgs = append(editArgs, "-status", s)
				}
			}
			if len(editArgs) == 1 {
				err = fmt.Errorf("nothing to update: enter at least one new value")
			} else {
				err = runEdit(a, editArgs)
				if err == nil {
					err = a.FlushSave()
				}
			}
		case "6", "search":
			if err = checkConnection(a); err != nil {
				break
			}
			if rest == "" {
				fmt.Print("keyword: ")
				if scanner.Scan() {
					rest = strings.TrimSpace(scanner.Text())
				}
			}
			if rest != "" {
				err = runSearch(a, []string{rest})
			} else {
				err = fmt.Errorf("keyword required")
			}
		case "7", "tag":
			if err = checkConnection(a); err != nil {
				break
			}
			args := strings.Fields(rest)
			if len(args) < 2 {
				if !showTasksBeforePrompt(a) {
					err = fmt.Errorf("no tasks")
					break
				}
				if rest != "" {
					fmt.Print("tag: ")
				} else {
					fmt.Print("id and tag (e.g. 1 work): ")
				}
				if scanner.Scan() {
					args = strings.Fields(scanner.Text())
				}
			}
			if len(args) >= 2 {
				err = runTag(a, []string{"add", args[0], strings.Join(args[1:], " ")})
				if err == nil {
					err = a.FlushSave()
				}
			} else {
				err = fmt.Errorf("id and tag required")
			}
		case "8", "reset":
			if err = checkConnection(a); err != nil {
				break
			}
			err = runReset(a, nil)
			if err == nil {
				err = a.FlushSave()
			}
		default:
			fmt.Printf("Unknown choice: %q\n", choice)
			continue
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", connectionErrMsg(err))
		}
		fmt.Println()
	}
}

// showTasksBeforePrompt prints the task list with JSON field names so the user can pick an id (before done/edit/delete/tag).
// Returns false if there are no tasks or on error (e.g. connection lost).
func showTasksBeforePrompt(a *app.App) bool {
	tasks, err := a.ListTasks(app.FilterAll, app.SortByCreatedAt, false, false)
	if err != nil || len(tasks) == 0 {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", connectionErrMsg(err))
		} else if len(tasks) == 0 {
			fmt.Println("No tasks.")
		}
		return false
	}
	fmt.Printf("%-5s %-18s %-12s %-8s %-12s %-18s %-12s\n", "id", "title", "description", "priority", "due_date", "tags", "status")
	fmt.Println(strings.Repeat("-", 95))
	for _, t := range tasks {
		pri := t.Priority
		if pri == "" {
			pri = "med"
		}
		due := "-"
		if t.DueDate != nil {
			due = t.DueDate.Format("2006-01-02")
		}
		tagsStr := "-"
		if len(t.Tags) > 0 {
			tagsStr = strings.Join(t.Tags, ", ")
			if len(tagsStr) > 16 {
				tagsStr = tagsStr[:13] + "..."
			}
		}
		title := t.Title
		if len(title) > 16 {
			title = title[:13] + "..."
		}
		desc := t.Description
		if desc == "" {
			desc = "-"
		} else if len(desc) > 10 {
			desc = desc[:7] + "..."
		}
		fmt.Printf("%-5d %-18s %-12s %-8s %-12s %-18s %-12s\n", t.ID, title, desc, pri, due, tagsStr, t.Status)
	}
	fmt.Println()
	return true
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
	dueDate, err := parseDueDate(*due)
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

	// Table headers match JSON field names (as stored)
	fmt.Printf("%-5s %-18s %-12s %-8s %-12s %-18s %-12s\n", "id", "title", "description", "priority", "due_date", "tags", "status")
	fmt.Println(strings.Repeat("-", 95))
	for _, t := range tasks {
		pri := t.Priority
		if pri == "" {
			pri = "med"
		}
		due := "-"
		if t.DueDate != nil {
			due = t.DueDate.Format("2006-01-02")
		}
		tagsStr := "-"
		if len(t.Tags) > 0 {
			tagsStr = strings.Join(t.Tags, ", ")
			if len(tagsStr) > 16 {
				tagsStr = tagsStr[:13] + "..."
			}
		}
		title := t.Title
		if len(title) > 16 {
			title = title[:13] + "..."
		}
		desc := t.Description
		if desc == "" {
			desc = "-"
		} else if len(desc) > 10 {
			desc = desc[:7] + "..."
		}
		fmt.Printf("%-5d %-18s %-12s %-8s %-12s %-18s %-12s\n", t.ID, title, desc, pri, due, tagsStr, t.Status)
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
	task, _ := a.GetTask(id)
	if task == nil {
		return fmt.Errorf("task #%d not found", id)
	}
	if !*force {
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
	dueDate, err := parseDueDate(*due)
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
	fmt.Printf("%-5s %-12s %s\n", "id", "status", "title")
	fmt.Println(strings.Repeat("-", 50))
	for _, t := range tasks {
		fmt.Printf("%-5d %-12s %s\n", t.ID, t.Status, t.Title)
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
	added, err := a.TagAdd(id, tag)
	if err != nil {
		return err
	}
	if added {
		fmt.Printf("Added tag %q to task #%d.\n", tag, id)
	} else {
		fmt.Printf("Tag %q already present on task #%d.\n", tag, id)
	}
	return nil
}

func runReset(a *app.App, args []string) error {
	fs := flag.NewFlagSet("reset", flag.ExitOnError)
	force := fs.Bool("force", false, "skip confirmation")
	_ = fs.Parse(args)
	tasks, err := a.ListTasks(app.FilterAll, app.SortByCreatedAt, false, false)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("No tasks to reset.")
		return nil
	}
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

func isOnlyDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func containsNoLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return false
		}
	}
	return true
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

const maxDueDateYears = 10

// parseDueDate parses YYYY-MM-DD and returns an error if the date is in the past or more than 10 years ahead.
func parseDueDate(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, fmt.Errorf("invalid date (use YYYY-MM-DD): %w", err)
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dueDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
	if dueDay.Before(today) {
		return nil, fmt.Errorf("due date cannot be in the past (use today or a future date)")
	}
	maxDue := now.AddDate(maxDueDateYears, 0, 0)
	maxDay := time.Date(maxDue.Year(), maxDue.Month(), maxDue.Day(), 0, 0, 0, 0, now.Location())
	if dueDay.After(maxDay) {
		return nil, fmt.Errorf("due date cannot be more than %d years in the future", maxDueDateYears)
	}
	return &t, nil
}
