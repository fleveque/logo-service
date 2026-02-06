# Go Learnings

Notes and concepts learned while building the logo-service, phase by phase.

---

## Phase 1: Project Skeleton

**Packages and entry points**
- Every `.go` file starts with `package name`. The `main` package with a `main()` function is the program entry point
- Go compiles to a single static binary — no runtime or interpreter needed
- Imports are organized in groups: stdlib, then third-party, then internal packages

**Structs and tags**
- Go uses structs instead of classes. Struct tags (backtick annotations like `` `json:"name"` ``) control how libraries serialize fields
- Example: `Port int \`mapstructure:"port"\`` tells Viper how to map YAML keys to struct fields

**Error handling**
- No exceptions. Functions return `(result, error)` pairs; callers check `if err != nil { ... }`
- `fmt.Errorf("context: %w", err)` wraps errors while preserving the chain for `errors.Is()` checks

**Goroutines and channels**
- `go func() { ... }()` spawns a goroutine — a lightweight concurrent "thread" managed by the Go runtime (not an OS thread)
- Channels (`make(chan Type, bufferSize)`) are typed pipes for goroutine communication — Go's motto: "share memory by communicating"
- `select` is like a switch for channels — it blocks until one is ready

**`defer`**
- `defer someFunc()` runs when the enclosing function returns — like Ruby's `ensure` or a `finally` block
- Deferred calls execute in LIFO order (last deferred = first executed)
- Gotcha: `defer` doesn't run when `os.Exit()` is called — that's why we use a separate `run()` function

**Method receivers**
- `func (s *Server) Start() error` attaches a method to a struct — `s` is like `self` or `this`
- Pointer receivers (`*Server`) can mutate the struct; value receivers (`Server`) work on a copy

**Linting**
- `errcheck` lint rule requires all error returns to be checked
- `_ = someFunc()` explicitly discards an error you've decided is safe to ignore
- `defer func() { _ = logger.Sync() }()` — wrapping in an anonymous function lets you handle the return value

---

## Phase 2: Storage Layer

**Implicit interfaces**
- Go interfaces are satisfied implicitly — a type just needs to have matching methods, no `implements` keyword
- This is called "structural typing" (vs. Java/Ruby's "nominal typing")
- Common pattern: **export the interface, hide the implementation**
  ```go
  // Public interface
  type LogoRepository interface { GetBySymbol(ctx, symbol) (*Logo, error) }
  // Private implementation
  type sqliteLogoRepository struct { db *sqlx.DB }
  ```

**Exported vs unexported**
- Capitalization controls visibility: `LogoRepository` (capital) = public, `sqliteLogoRepository` (lowercase) = package-private
- This applies to functions, types, struct fields, methods — everything

**Sentinel errors**
- `var ErrNotFound = errors.New("logo not found")` — a predefined error value
- Callers check with `errors.Is(err, ErrNotFound)` — this works through the entire error chain (thanks to `%w` wrapping)

**Blank imports**
- `_ "github.com/mattn/go-sqlite3"` imports a package only for its `init()` side effect
- The `go-sqlite3` package registers itself as a `database/sql` driver in its `init()` function

**Testing**
- Test files must end with `_test.go` — they're excluded from production builds
- Test functions must start with `Test` and take `*testing.T`
- `t.TempDir()` creates a temp directory auto-cleaned after the test — no manual teardown
- `t.Cleanup(func)` registers functions to run when the test finishes
- `t.Helper()` marks a function as a test helper — error line numbers point to the caller instead
- `t.Fatal()` stops immediately; `t.Error()` continues to find more failures

**SQLite pragmas**
- WAL mode (`_journal_mode=WAL`): allows concurrent reads while writing
- `_busy_timeout=5000`: wait up to 5s instead of failing on lock contention
- `SetMaxOpenConns(1)`: SQLite performs best with a single writer
