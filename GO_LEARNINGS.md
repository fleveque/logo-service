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

---

## Phase 3: Image Processing

**CGO and C bindings**
- `bimg` wraps libvips via CGO — Go's mechanism for calling C code
- Trade-off: amazing performance (libvips is one of the fastest image libraries) but requires a C library at build time
- This is why CI needs `apt-get install libvips-dev` and the Dockerfile needs a multi-stage build
- CGO compiles slower than pure Go — that's why we increased the lint timeout to 5 minutes

**Options structs (alternative to builders)**
- Go doesn't use builder patterns much. Instead, you pass a struct with optional fields:
  ```go
  img.Process(bimg.Options{
      Width:  64,
      Height: 64,
      Type:   bimg.PNG,
      Embed:  true,
  })
  ```
- Only set the fields you need — zero values are meaningful defaults

**Table-driven tests**
- The idiomatic way to test multiple inputs in Go: define test cases as a slice of structs, loop with `t.Run()`
  ```go
  tests := []struct {
      name    string
      input   string
      want    int
      wantErr bool
  }{
      {"valid", "ff0000", 255, false},
      {"invalid", "xyz", 0, true},
  }
  for _, tt := range tests {
      t.Run(tt.name, func(t *testing.T) { ... })
  }
  ```
- Each `t.Run()` creates a named subtest — shows up individually in test output and can be run in isolation

**Standard library image support**
- Go's `image` and `image/png` packages can create and encode images without any dependencies
- Useful for generating test fixtures: `image.NewNRGBA()` creates an in-memory image you can manipulate pixel by pixel
- `bytes.Buffer` implements `io.Writer` — you can encode a PNG directly into a byte buffer

**`fmt.Sscanf`**
- Like C's `scanf` — parses formatted strings: `fmt.Sscanf("ff0000", "%02x%02x%02x", &r, &g, &b)`
- Go inherited several C-isms: `fmt.Sprintf`, `fmt.Fprintf`, `fmt.Sscanf` all follow C format strings

---

## Phase 4: Core API (Handlers, Auth, Rate Limiting, CORS)

**Closures for middleware factories**
- Middleware constructors return `gin.HandlerFunc` — the outer function captures config in its closure:
  ```go
  func APIKeyAuth(validKeys []string) gin.HandlerFunc {
      keySet := buildSet(validKeys) // captured in closure
      return func(c *gin.Context) {
          if _, ok := keySet[key]; !ok { c.Abort() }
      }
  }
  ```
- The returned function has access to `keySet` even though `APIKeyAuth` has already returned — that's the closure

**`map[string]struct{}` as a Set**
- Go has no built-in Set type. The idiomatic substitute is `map[string]struct{}`
- `struct{}` takes zero bytes of memory, so the map only stores keys — perfect for membership checks
- Check membership with `if _, ok := set[key]; ok { ... }`

**Type assertions**
- `key.(string)` asserts that an `interface{}` (aka `any`) value is actually a `string`
- Panics if wrong — use the two-value form `val, ok := key.(string)` for safe assertions

**`sync.Mutex` for shared state**
- Goroutines sharing a map need synchronization. `sync.Mutex` is simplest:
  ```go
  var mu sync.Mutex
  mu.Lock()
  limiters[key] = newLimiter
  mu.Unlock()
  ```
- Use mutex for simple shared state; use channels for complex coordination

**`httptest` for handler testing**
- `httptest.NewRecorder()` captures HTTP responses without a real server
- `httptest.NewRequest()` creates fake requests — set headers, query params, body
- Combined with `router.ServeHTTP(w, req)`, you test the full middleware + handler chain in-process
- Tests run in milliseconds with no network I/O

**Gin route groups**
- `r.Group("/api/v1")` creates a group with a shared prefix
- `.Use(middleware)` applies to all routes in the group
- Groups can be nested: `api.Group("/admin")` adds middleware only for admin routes
- Curly braces `{ ... }` around routes are cosmetic (scoping convention, not required by Go)
