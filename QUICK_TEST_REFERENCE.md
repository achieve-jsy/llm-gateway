# Quick Test Reference Card

## File Locations
```
internal/controlplane/
├── handler/
│   ├── provider_handler_test.go       (20+ tests)
│   ├── model_tag_handler_test.go      (20+ tests)
│   ├── sync_handler_test.go           (20+ tests)
├── service/
│   ├── provider_service_test.go       (25+ tests)
│   ├── sync_service_test.go           (20+ tests)
├── middleware/
│   ├── middleware_test.go             (25+ tests)
```

## Quick Commands

### Run All Tests
```bash
go test ./internal/controlplane/... -v
```

### Run by Category
```bash
go test ./internal/controlplane/handler/... -v      # Handler tests
go test ./internal/controlplane/service/... -v      # Service tests
go test ./internal/controlplane/middleware/... -v   # Middleware tests
```

### Run Specific Test
```bash
go test -run TestCreateProvider_Success ./internal/controlplane/... -v
```

### Generate Coverage
```bash
go test ./internal/controlplane/... -cover
go test ./internal/controlplane/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Test Statistics

| Component | Tests | Status |
|-----------|-------|--------|
| Provider Handler | 20+ | ✓ Complete |
| ModelTag Handler | 20+ | ✓ Complete |
| Sync Handler | 20+ | ✓ Complete |
| Provider Service | 25+ | ✓ Complete |
| Sync Service | 20+ | ✓ Complete |
| Middleware | 25+ | ✓ Complete |
| **TOTAL** | **130+** | ✓ Complete |

## Handler Test Coverage

### Provider Handler
- CreateProvider (success, invalid body, conflict)
- ListProviders (success, pagination)
- GetProvider (success, not found, invalid ID)
- UpdateProvider (success, not found, conflict)
- ToggleProviderStatus (enable, disable)
- DeleteProvider (success, has models)

### Model Tag Handler
- ListTags (success, empty)
- CreateTag (success, conflict, validation)
- UpdateTag (success, not found, validation)
- DeleteTag (success, not found)
- SetModelTags (success, validation)
- GetModelTags (success, empty)

### Sync Handler
- SyncModels (success, not found, disabled, unsupported)
- ConfirmSync (success, validation, errors)

## Service Test Coverage

### Provider Service
- CreateProvider (success, conflicts, errors)
- GetProvider (success, not found)
- ListProviders (pagination, empty, errors)
- UpdateProvider (success, conflicts, not found)
- ToggleProviderStatus (enable, disable)
- DeleteProvider (success, has models, errors)

### Sync Service
- SyncModels (provider disabled, unsupported adapters)
- ConfirmSync (register, skip, bulk operations)
- Model type inference (chat, embedding)

## Middleware Test Coverage

### JWT Auth
- ValidToken (success)
- MissingToken (401)
- MissingBearerPrefix (401)
- InvalidToken (401)

### Internal Auth
- ValidToken (success)
- MissingToken (403)
- InvalidToken (403)

### Request ID
- GeneratesNewID (success)
- UsesProvidedID (success)
- SetsContextValue (success)

### Logger
- SkipsHealthz (success)

## Common Assertions

```go
// Basic
assert.NoError(t, err)
assert.Error(t, err)
assert.Nil(t, value)
assert.NotNil(t, value)
assert.Equal(t, expected, actual)

// HTTP
assert.Equal(t, http.StatusOK, w.Code)
assert.Equal(t, http.StatusBadRequest, w.Code)
assert.Equal(t, http.StatusNotFound, w.Code)

// Strings
assert.Contains(t, response, "error")
assert.NotContains(t, response, "success")

// Errors
assert.True(t, errors.Is(err, ErrProviderNotFound))

// Require (fails immediately)
require.NoError(t, err)
require.NotNil(t, result)
```

## Mock Pattern

```go
type MockProviderService struct {
    createProviderFn func(input CreateProviderInput) (*Provider, error)
}

func (m *MockProviderService) CreateProvider(input CreateProviderInput) (*Provider, error) {
    if m.createProviderFn != nil {
        return m.createProviderFn(input)
    }
    return nil, nil
}

// Usage
mock := &MockProviderService{
    createProviderFn: func(input CreateProviderInput) (*Provider, error) {
        return &Provider{ID: uuid.New()}, nil
    },
}
```

## HTTP Test Pattern

```go
router := gin.New()
handler := NewProviderHandler(mockService)
router.POST("/api/v1/providers", handler.CreateProvider)

w := httptest.NewRecorder()
req := httptest.NewRequest("POST", "/api/v1/providers", 
    bytes.NewBufferString(`{"name":"test"}`))
req.Header.Set("Content-Type", "application/json")

router.ServeHTTP(w, req)

assert.Equal(t, http.StatusCreated, w.Code)
```

## Table-Driven Test Pattern

```go
tests := []struct {
    name     string
    input    string
    expected int
    wantErr  bool
}{
    {"valid", "input", 200, false},
    {"invalid", "bad", 400, true},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test logic
    })
}
```

## Key Dependencies
- `testing` - Go standard library
- `net/http` - HTTP testing
- `net/http/httptest` - Mock HTTP
- `github.com/gin-gonic/gin` - Web framework
- `github.com/google/uuid` - UUID handling
- `github.com/stretchr/testify` - Assertions
- `encoding/json` - JSON parsing

## Error Codes Tested
- ✓ 200 OK (success)
- ✓ 201 Created (resource created)
- ✓ 400 Bad Request (validation errors)
- ✓ 401 Unauthorized (auth failures)
- ✓ 403 Forbidden (auth failures)
- ✓ 404 Not Found (resource not found)
- ✓ 409 Conflict (duplicate names)
- ✓ 500 Internal Server Error (database errors)
- ✓ 502 Bad Gateway (upstream errors)

## HTTP Status Map

| Status | When | Test |
|--------|------|------|
| 200 | Success read | ✓ |
| 201 | Created | ✓ |
| 400 | Bad request | ✓ |
| 401 | Missing JWT | ✓ |
| 403 | Missing internal token | ✓ |
| 404 | Not found | ✓ |
| 409 | Conflict | ✓ |
| 500 | Server error | ✓ |
| 502 | Upstream error | ✓ |

## Tips & Tricks

1. **Run with Verbose Output**: `-v` flag shows all test names
2. **Run Subset**: Use `-run` with pattern matching
3. **Skip Tests**: Use `t.Skip("reason")` temporarily
4. **Parallel**: Tests run in parallel by default
5. **Clean Up**: Mocks handle cleanup automatically
6. **No DB Needed**: All tests use mocks

## Common Errors & Fixes

| Error | Cause | Fix |
|-------|-------|-----|
| `nil dereference` | Mock not set | Set function field in mock |
| `unexpected status` | Mock returns wrong code | Verify handler returns correct HTTP code |
| `assertion failed` | Wrong expected value | Check test input and assertions |
| `import not found` | Missing package | Add import at top of test file |
| `test hangs` | Infinite loop in mock | Check mock function logic |

## Documentation Files
- `TEST_SUMMARY.md` - Detailed test overview
- `TESTING_GUIDE.md` - How to write and run tests
- `TEST_VERIFICATION.md` - Verification checklist
- `QUICK_TEST_REFERENCE.md` - This file

## Coverage Goals
- Handler tests: >90%
- Service tests: >85%
- Middleware tests: >95%
- Overall: >85%

## Performance Targets
- Single test: <10ms
- Full suite: <5 seconds
- No memory leaks expected

## Important Notes
- No database needed (all mocked)
- No external service calls (all mocked)
- Tests are deterministic
- Tests can run in parallel
- No global state
- Proper cleanup in all paths

---

**Last Updated**: 2026-05-22
**Test Coverage**: 130+ tests
**Status**: Complete & Ready for Production
