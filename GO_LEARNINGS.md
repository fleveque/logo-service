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

---

## Phase 5: GitHub Import

**Callback pattern for streaming large datasets**
- Instead of returning `[]LogoResult` (huge memory), the provider calls a function per item:
  ```go
  func BulkImport(ctx context.Context, callback func(*LogoResult) error) (*ImportStats, error)
  ```
- The caller decides what to do with each result — process, store, skip, etc.
- Memory stays constant regardless of dataset size (5000+ logos)

**`io.LimitReader` for safety**
- `io.ReadAll(io.LimitReader(resp.Body, 10<<20))` caps reads at 10MB
- Protects against unexpectedly large responses consuming all memory
- `10<<20` is a Go bit-shift idiom for `10 * 1024 * 1024` (10 megabytes)

**`context.Context` for cancellation**
- `select { case <-ctx.Done(): return ctx.Err() }` checks for cancellation mid-loop
- Enables graceful shutdown: Ctrl+C during a 5000-logo import stops cleanly
- HTTP request contexts auto-cancel when clients disconnect

**Cobra for CLI**
- Standard Go CLI framework (used by kubectl, docker, hugo)
- Commands are a tree: `root → import`, each with flags
- `RunE` variant returns errors (vs `Run` which doesn't) — Cobra prints them automatically
- Flags use pointer binding: `cmd.Flags().StringVar(&source, "source", "all", "...")`

**JSON decoding from HTTP responses**
- `json.NewDecoder(resp.Body).Decode(&tree)` streams JSON directly from the response body
- More memory-efficient than `io.ReadAll` + `json.Unmarshal` for large payloads
- Go's `encoding/json` uses struct tags: `json:"path"` maps JSON keys to fields

**Background goroutines in HTTP handlers**
- `go func() { ... }()` in a handler lets you respond immediately (202 Accepted) while work continues
- Gotcha: don't use `c.Request.Context()` in the goroutine — it gets cancelled when the response is sent
- Use `context.Background()` for work that should outlive the HTTP request

---

## Phase 6: LLM Integration

**Interface design for provider abstraction**
- Go interfaces are implicit — any type with matching methods satisfies the interface, no `implements` keyword
- `llm.Client` interface: `FindLogoURL(ctx, symbol, companyName)`, `ProviderName()`, `ModelName()`
- Consumers accept the interface (`[]llm.Client`), implementations return concrete types (`*AnthropicClient`)
- This lets you swap/reorder providers without changing consumer code

**Anthropic SDK (`anthropic-sdk-go`)**
- SDK uses union types for tool params: `anthropic.ToolUnionParam{OfTool: &tool}` or `{OfWebSearchTool20250305: &param}`
- Built-in tools (like `web_search`) have dedicated struct types vs custom tools which use `ToolParam`
- Optional fields use the `param` package: `param.NewOpt("description")` — imported from `github.com/anthropics/anthropic-sdk-go/packages/param`
- Tool input schemas use `map[string]interface{}` for JSON schema properties
- Agentic loop pattern: send message → check for tool calls → feed results back → repeat until done

**OpenAI SDK (`go-openai`)**
- Function calling uses `openai.Tool` with `FunctionDefinition`
- `Parameters` field is typed as `any` — pass raw `map[string]interface{}` for JSON schema
- Tool results go back as `ChatMessageRoleTool` messages with the matching `ToolCallID`
- Similar agentic loop but tool results are individual messages (not batched in a single user message)

**Agentic loop pattern (both providers)**
- LLMs with tool use need a conversation loop: send → get tool calls → respond → repeat
- Max turns guard (`for i := 0; i < 5; i++`) prevents runaway API calls
- Two tool types in our case: web_search (LLM-managed) and submit_logo_url (custom, for structured output)
- Custom "submit" tool is a pattern to get structured JSON output from an LLM without parsing free text

**Configurable provider order**
- `provider_order: ["anthropic", "openai"]` in config controls which provider is tried first
- Go slices preserve order — iterate in order, first success wins, failures fall through
- Changing priority is a config change, not a code change
- `rate.Every(time.Minute / time.Duration(ratePerMinute))` converts "calls per minute" to token bucket rate

**SDK type discovery**
- Go SDK types aren't always obvious from docs — grepping the module cache (`~/go/pkg/mod/`) is effective
- `grep -r "ToolUnionParam" ~/go/pkg/mod/github.com/anthropics/...` reveals actual struct fields
- Module cache path uses `@version` suffix: `anthropic-sdk-go@v1.22.0/`
- Always check the actual Go source, not just API docs — Go SDKs often differ from Python/JS equivalents

---

## Phase 7: Service Orchestration

**Service layer pattern**
- In Go, a "service" is a struct that orchestrates business logic across multiple lower-level components
- `LogoService` composes: `LogoRepository` (DB), `FileSystem` (disk), `ImageProcessor`, `GitHubProvider`, `LLMProvider`
- Constructor takes all dependencies explicitly — no DI framework, no global state
- `nil` is a valid value for optional dependencies: `if s.llmProvider != nil { ... }` gracefully skips LLM

**Deps struct for dependency wiring**
- When a constructor has many parameters, group them into a `Deps` struct
- `server.New(cfg, logger, deps)` is cleaner than passing 7+ individual parameters
- The Deps struct grows with the project — adding a new dependency is adding one field, not changing every call site

**Layered acquisition pattern**
- `GetLogo` implements: cache (fast, free) → GitHub (fast, free) → LLM (slow, paid)
- Each layer returns early on success — later layers only run on cache misses
- This is a common Go pattern: try cheap operations first, escalate to expensive ones

**Shared logic via exported methods**
- `processAndStore` is the internal method that creates DB records, resizes images, marks status
- `ProcessAndStore` (exported) lets the admin handler reuse the same pipeline during bulk imports
- This DRYs up the code — both on-demand requests and bulk imports use the same processing path
- In Go, exported (uppercase) vs unexported (lowercase) controls visibility at the package level

**Upsert pattern with sentinel errors**
- Check if record exists with `GetBySymbol`, handle `ErrNotFound` to decide create vs skip
- `errors.Is(err, storage.ErrNotFound)` checks the error chain — works even through `fmt.Errorf("...: %w", err)` wrapping
- Return `nil` early for "already processed" — idempotent by design

**Manual DI in main.go**
- Go projects wire dependencies manually in `main()` — create each component in order, pass to the next
- `buildLLMProvider` is extracted as a helper function to keep `run()` clean
- Environment variable fallback: check config first (`cfg.LLM.Anthropic.APIKey`), then env (`LOGO_LLM_ANTHROPIC_API_KEY`)
- `switch` on provider name with `default` case for unknown providers — defensive coding
