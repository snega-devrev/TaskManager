# Task Manager

A **task manager** with a **command-line interface (CLI)** and an optional **gRPC API**. Tasks are stored in **MongoDB**. You can use the interactive menu, run commands from the shell, or call the API (with login) from other apps.

---

## What is this?

This repo is a Go application that lets you create and manage tasks (title, due date, priority, tags, status). Data is persisted in MongoDB. You can run it in two ways:

- **CLI only** – Interactive menu or shell commands; one shared task list, no login.
- **CLI + gRPC** – Same CLI plus a gRPC server; users register/login and get per-user task lists; you can use the API from other apps (e.g. with `grpcurl`).

The main entry point is **`main.go`**: it parses flags, starts the CLI (interactive or subcommands), and optionally starts the gRPC server when `-grpc-addr` is set.

---

## How to run the app

1. **Install Go 1.21+** and have **MongoDB** running (e.g. `brew services start mongodb-community` on macOS).
2. **Build:** `go build -o task .`
3. **Run CLI (interactive menu):**
   ```bash
   ./task -mongo-uri "mongodb://localhost:27017"
   ```
4. **Run CLI + gRPC server** (optional; for API access):
   ```bash
   export JWT_SECRET=$(openssl rand -hex 32)
   ./task -mongo-uri "mongodb://localhost:27017" -grpc-addr ":50051" -jwt-secret "$JWT_SECRET"
   ```

You can also set `MONGO_URI` instead of passing `-mongo-uri`. For more options (subcommands, `-user`, API usage), see the sections below.

---

## What you need

- **Go** 1.21 or later
- **MongoDB** running (locally or a URI you can reach)

---

## Quick start (5 steps)

### Step 1: Start MongoDB

Make sure MongoDB is running on your machine.

```bash
# macOS (Homebrew)
brew services start mongodb-community

# Or run in the foreground
mongod
```

### Step 2: Open the project and build

```bash
cd /path/to/taskmanager_mongo
go mod tidy
go build -o task .
```

### Step 3: Run the CLI (interactive menu)

```bash
./task -mongo-uri "mongodb://localhost:27017"
```

You’ll see a menu: **Add task**, **List tasks**, **Update status**, **Delete task**, **Edit task**, **Search**, **Tag add**, **Reset all**, **Exit**. Use the numbers or names to manage tasks. Data is saved to MongoDB automatically.

**Using a different MongoDB URI:** Set it once, then run without the flag:

```bash
export MONGO_URI="mongodb://localhost:27017"
./task
```

### Step 4 (optional): Run the gRPC server so others can use the API

In a **separate terminal**, keep the app running with gRPC and login enabled:

```bash
cd /path/to/taskmanager_mongo

# Create a secret (do this once, keep it private)
export JWT_SECRET=$(openssl rand -hex 32)

# Start app with gRPC on port 50051
./task -mongo-uri "mongodb://localhost:27017" -grpc-addr ":50051" -jwt-secret "$JWT_SECRET"
```

Leave this running. The same app now serves the interactive menu **and** the gRPC API.

### Step 5 (optional): Use the API — register, login, then call tasks

Install **grpcurl** (only if you want to try the API from the command line):

```bash
brew install grpcurl   # macOS
```

In **another terminal**:

**1. Register a user** (password at least 6 characters):

```bash
grpcurl -plaintext -d '{"username":"alice","password":"secret123"}' localhost:50051 taskmanager.AuthService/Register
```

Copy the `"token"` from the response.

**2. Use the token for task calls:**

```bash
# Set your token (paste the one from Register or Login)
export TOKEN="eyJ..."

# List tasks
grpcurl -plaintext -H "Authorization: Bearer $TOKEN" localhost:50051 taskmanager.TaskService/ListTasks

# Add a task
grpcurl -plaintext -H "Authorization: Bearer $TOKEN" -d '{"title":"My first task"}' localhost:50051 taskmanager.TaskService/AddTask
```

**3. Next time, log in instead of registering:**

```bash
grpcurl -plaintext -d '{"username":"alice","password":"secret123"}' localhost:50051 taskmanager.AuthService/Login
```

Use the new token the same way as above.

---

## Two ways to run

| What you want              | Command |
|----------------------------|--------|
| **CLI only** (menu + commands) | `./task -mongo-uri "mongodb://localhost:27017"` |
| **CLI + gRPC API** (with login) | `./task -mongo-uri "mongodb://localhost:27017" -grpc-addr ":50051" -jwt-secret "$JWT_SECRET"` |

- **CLI only:** One shared task list (stored in MongoDB as document `tasklist`). No login.
- **CLI + gRPC:** Each user has their own task list. Users register/login via the API and send the returned token with every request.

---

## CLI: same data as a gRPC user (`-user`)

If you run the app with gRPC and login, each user has a separate list. You can use the **CLI** to work on a specific user’s list with the **`-user`** flag:

```bash
./task -mongo-uri "mongodb://localhost:27017" -grpc-addr ":50051" -jwt-secret "$JWT_SECRET" -user alice
```

Then the interactive menu (add, list, edit, etc.) uses **alice’s** task list — the same data you see when you call the API with alice’s token. Omit `-user` to use the default shared list.

---

## CLI commands (non-interactive)

You can run commands directly instead of using the menu:

```bash
# Add tasks
./task -mongo-uri "mongodb://localhost:27017" add "Buy groceries"
./task -mongo-uri "mongodb://localhost:27017" add -title "Finish report" -due 2026-02-20 -priority high -tag work

# List
./task -mongo-uri "mongodb://localhost:27017" list
./task -mongo-uri "mongodb://localhost:27017" list -pending
./task -mongo-uri "mongodb://localhost:27017" list -sort due -json

# Other commands
./task -mongo-uri "mongodb://localhost:27017" done 1
./task -mongo-uri "mongodb://localhost:27017" delete 2
./task -mongo-uri "mongodb://localhost:27017" edit 1 -title "New title" -status in-progress
./task -mongo-uri "mongodb://localhost:27017" search "report"
./task -mongo-uri "mongodb://localhost:27017" tag add 1 work
./task -mongo-uri "mongodb://localhost:27017" reset
```

With `-user alice`, add the same flag to these commands to act as that user.

---

## Features

- **Interactive menu** – Add, list, update status, delete, edit, search, tag, reset
- **Task fields** – Title, description, priority (low/med/high), due date, tags, status (todo/in-progress/done)
- **Filters & sort** – List all, done, or pending; due today, overdue; sort by due date, priority, or created
- **Autosave** – Changes are saved to MongoDB after a short delay
- **gRPC API** – Register and login; get a token; use it for all task operations (per-user lists)
- **CLI `-user`** – Use the CLI on a specific user’s list (same as gRPC for that user)

---

## Troubleshooting

- **“connection refused” on port 50051**  
  The gRPC server is not running. Start the app with `-grpc-addr ":50051"` (and `-jwt-secret`) in a terminal and leave it running.

- **“invalid or expired token”**  
  Get a new token: call **Login** again and use the new token. Tokens expire after 7 days.

- **“login required” when calling TaskService**  
  Send the token in the header: `-H "Authorization: Bearer $TOKEN"`.

- **MongoDB connection errors**  
  Ensure MongoDB is running and `-mongo-uri` (or `MONGO_URI`) is correct (e.g. `mongodb://localhost:27017`).

- **“context deadline exceeded” or timeout from another machine**  
  See **[Fix: Can’t connect from another machine](#fix-cant-connect-from-another-machine)** below.

### Fix: Can’t connect from another machine

When `grpcurl` from the other machine gives **“context deadline exceeded”**, the client cannot reach port 50051 on the server. Do these steps in order.

**All steps on the SERVER machine (192.168.68.148) first:**

1. **Confirm the app is running and listening**
   - Start: `./task -mongo-uri "mongodb://localhost:27017" -grpc-addr ":50051" -jwt-secret "$JWT_SECRET"`
   - You must see: `gRPC server listening on 0.0.0.0:50051 (accepting connections from other machines)`

2. **Confirm port 50051 is open**
   ```bash
   lsof -i :50051
   ```
   You should see a line with `task` (or the process that runs `task`) and `*:50051` or `0.0.0.0:50051`. If nothing appears, the app is not listening; restart it.

3. **Test from the server (localhost)**
   ```bash
   grpcurl -plaintext -d '{"username":"bob","password":"bobpass123"}' localhost:50051 taskmanager.AuthService/Register
   ```
   If this **fails**, the app or MongoDB is the problem. If it **succeeds**, the app is fine; the issue is reaching the server from the other machine.

4. **Temporarily turn OFF the firewall (to confirm it’s the cause)**
   - **System Settings → Network → Firewall**
   - Turn the firewall **Off** (temporarily).
   - From the **other** machine run:
     ```bash
     grpcurl -plaintext -d '{"username":"bob","password":"bobpass123"}' 192.168.68.148:50051 taskmanager.AuthService/Register
     ```
   - If it **works** with the firewall off → the firewall was blocking. Go to step 5.
   - If it **still fails** → check step 6 (same network / correct IP). Then turn the firewall back **On** and use step 5 to allow the app.

5. **Turn the firewall back On and allow the app**
   - **System Settings → Network → Firewall** → turn **On**.
   - Click **Options** (or **Firewall Options**).
   - Click **+** and add:
     - **Terminal** from `/Applications/Utilities/Terminal.app` (if you run `./task` from Terminal), **or**
     - The **task** binary: go to your project folder (e.g. `taskmanager_mongo`) and select the **task** file.
   - Set that app to **Allow incoming connections**.
   - Click **OK**.
   - Test again from the other machine (same `grpcurl` command as in step 4).

6. **If it still fails: same network and correct IP**
   - **Same Wi‑Fi:** Both Macs must be on the same Wi‑Fi (or same LAN).
   - **Correct server IP:** On the server run `ifconfig | grep "inet " | grep -v 127.0.0.1` and use that IP (e.g. 192.168.68.148) on the client. Don’t use `localhost` from the other machine.
   - **Test port from the other machine** (optional):
     ```bash
     nc -zv 192.168.68.148 50051
     ```
     If this times out or fails, the port is still blocked (firewall) or the server isn’t reachable (network). If it succeeds, `grpcurl` should work too.

7. **If nothing works: router may block device-to-device traffic**
   - Some Wi‑Fi routers have **AP isolation** (or **client isolation**) that stops devices from talking to each other. Check the router’s admin page (e.g. 192.168.68.1) for a setting like “AP isolation” or “Allow client-to-client” and disable isolation / allow client communication.
   - **Workaround – SSH tunnel:** From the **other** machine, create a tunnel so that localhost:50051 on that machine forwards to the server’s 50051:
     ```bash
     ssh -L 50051:localhost:50051 admin@192.168.68.148
     ```
     Leave that SSH session open. Then on the other machine run:
     ```bash
     grpcurl -plaintext -d '{"username":"bob","password":"bobpass123"}' localhost:50051 taskmanager.AuthService/Register
     ```
     (Use `localhost` because the tunnel forwards to the server.) You need SSH login (e.g. password or key) to the server.
   - **Workaround – ngrok (if you can install it):** On the server run `ngrok tcp 50051` and use the public address ngrok gives you (e.g. `0.tcp.ngrok.io:12345`) from the other machine. See [ngrok](https://ngrok.com).

---

## JWT secret and tokens

When you run with **`-grpc-addr`** and **`-jwt-secret`**:

- **JWT secret:** You choose it. Example: `export JWT_SECRET=$(openssl rand -hex 32)`. Use the same value every time you start the server. Don’t commit it.
- **Tokens:** Users get a token by calling **Register** or **Login**. They then send that token as `Authorization: Bearer <token>` for all task API calls. No need to create tokens by hand unless you’re testing (e.g. with `go run ./cmd/mint-token -user alice -secret "$JWT_SECRET"`).

---

## Connect from a different machine

To use the gRPC API from another computer (same Wi‑Fi/LAN or over the internet), do the following.

### On the machine where the app runs (server)

1. **Start the app with gRPC** as usual. Using `-grpc-addr ":50051"` makes it listen on all interfaces (not only localhost):

   ```bash
   ./task -mongo-uri "mongodb://localhost:27017" -grpc-addr ":50051" -jwt-secret "$JWT_SECRET"
   ```

2. **Find this machine’s IP address** (so the other machine can reach it):

   ```bash
   # macOS / Linux
   ifconfig | grep "inet " | grep -v 127.0.0.1
   # Or: ip addr  (Linux)
   ```

   Use the IP that looks like `192.168.x.x` or `10.x.x.x` (your LAN IP). Example: `192.168.1.10`.

3. **Allow port 50051 in the firewall** on this server machine:

   ```bash
   # macOS (if using pf)
   # Or: System Settings → Network → Firewall → allow the app or allow port 50051

   # Linux (ufw)
   sudo ufw allow 50051/tcp
   sudo ufw reload
   ```

### On the other machine (client)

1. **Install grpcurl** (if not already):

   ```bash
   brew install grpcurl   # macOS
   # Or: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
   ```

2. **Get a token** by logging in against the **server’s IP** (replace `SERVER_IP` with the IP from step 2 above, e.g. `192.168.1.10`):

   ```bash
   grpcurl -plaintext -d '{"username":"alice","password":"secret123"}' SERVER_IP:50051 taskmanager.AuthService/Login
   ```

   Copy the `"token"` from the response and set it:

   ```bash
   export TOKEN="<paste token here>"
   ```

3. **Call the API** using the server’s IP instead of `localhost`:

   ```bash
   grpcurl -plaintext -H "Authorization: Bearer $TOKEN" SERVER_IP:50051 taskmanager.TaskService/ListTasks
   grpcurl -plaintext -H "Authorization: Bearer $TOKEN" -d '{"title":"My task"}' SERVER_IP:50051 taskmanager.TaskService/AddTask
   ```

**Example** (server IP is 192.168.1.10):

```bash
grpcurl -plaintext -d '{"username":"snega","password":"yourpassword"}' 192.168.1.10:50051 taskmanager.AuthService/Login
export TOKEN="eyJ..."
grpcurl -plaintext -H "Authorization: Bearer $TOKEN" 192.168.1.10:50051 taskmanager.TaskService/ListTasks
```

**Note:** Traffic is plaintext. For production over the internet, put the server behind TLS (e.g. nginx or Caddy) and use the correct host/port in grpcurl.

---

## Build and proto

```bash
go build -o task .
```

To regenerate gRPC code after changing `api/proto/taskmanager.proto`:

```bash
make generate
```

(Requires `protoc` and the Go plugins; see the Makefile.)

---

## Project structure

- **`internal/domain`** – Task model and validation helpers
- **`internal/store`** – Storage interface and MongoDB implementation (including user-scoped lists)
- **`internal/app`** – Business logic (add, list, edit, etc.)
- **`internal/server/grpc`** – gRPC server (Auth: Register/Login; Task: AddTask, ListTasks, etc.)
- **`main.go`** – CLI (interactive menu and subcommands), starts gRPC server when `-grpc-addr` is set