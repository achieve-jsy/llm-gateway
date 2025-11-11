# Control-Plane Module Testing Guide

A comprehensive guide to the TDD unit tests for the control-plane module.

## Quick Start

### Run All Control-Plane Tests
```bash
cd /Users/jsy/Public/self_code/llm-gateway
go test ./internal/controlplane/... -v
```

### Run Tests by Category

**Handler Tests:**
```bash
go test ./internal/controlplane/handler/... -v
```

**Service Tests:**
```bash
go test ./internal/controlplane/service/... -v
```

**Middleware Tests:**
```bash
go test ./internal/controlplane/middleware/... -v
```

### Run Specific Tests
```bash
# Single test
go test ./internal/controlplane/handler/ -run TestCreateProvider_Success -v

# Multiple tests matching pattern
go test ./internal/controlplane/service/ -run TestProvider -v

# With coverage
go test ./internal/controlplane/... -cover -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Test Files Reference

### Handler Tests (60+ tests)

**File**: `internal/controlplane/handler/provider_handler_test.go`
- Tests all provider CRUD operations
- Validates HTTP status codes and error responses
- Uses Gin router for HTTP testing
- Mock service dependency

**File**: `internal/controlplane/handler/model_tag_handler_test.go`
- Tests tag management endpoints
- Validates input constraints (name/color length)
- Tests many-to-many tag relationships
- Tests conflict detection

**File**: `internal/controlplane/handler/sync_handler_test.go`
- Tests model synchronization endpoints
- Validates unsupported adapter detection
- Tests error status mapping (400/404/502)
- Uses mock service with custom behaviors

### Service Tests (45+ tests)

**File**: `internal/controlplane/service/provider_service_test.go`
- Tests business logic for provider management
- Validates credential encryption/masking
- Tests cascade delete constraints
- Tests pagination logic
- Uses mocked repositories

**File**: `internal/controlplane/service/sync_service_test.go`
- Tests model synchronization logic
- Tests model type inference (chat vs embedding)
- Tests bulk model registration
- Tests error handling and skipping
- Uses mocked repositories

### Middleware Tests (25+ tests)

**File**: `internal/controlplane/middleware/middleware_test.go`
- Tests request ID generation and propagation
- Tests JWT authentication
- Tests internal token validation
- Tests middleware chaining and ordering
- Validates error response formats

## Test Structure & Patterns

### Mock Pattern
All tests use interface-based mocks:

```go
type MockProviderRepo struct {
    createFn func(provider *model.Provider) error
    listFn   func(offset, limit int) ([]model.Provider, int64, error)
    // ... other methods
}

func (m *MockProviderRepo) Create(provider *model.Provider) error {
    if m.createFn != nil {
        return m.createFn(provider)
    }
    return nil
}
```

**Advantages**:
- No database dependency
- Fast test execution
- Flexible behavior configuration
- Easy to simulate error conditions

### Table-Driven Tests
For systematic testing of multiple scenarios:

```go
tests := []struct {
    name     string
    input    string
    expected int
    err      bool
}{
    {"valid case", "input", 200, false},
    {"invalid case", "bad", 400, true},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test logic
    })
}
```

### HTTP Testing
Handler tests use Gin and httptest:

```go
router := gin.New()
handler := NewProviderHandler(mockSvc)
router.POST("/api/v1/providers", handler.CreateProvider)

w := httptest.NewRecorder()
req := httptest.NewRequest("POST", "/api/v1/providers", 
    bytes.NewBufferString(`{"name":"test"}`))
req.Header.Set("Content-Type", "application/json")

router.ServeHTTP(w, req)

assert.Equal(t, http.StatusCreated, w.Code)
```

## Common Test Scenarios

### Testing Success Cases
```go
func TestCreateProvider_Success(t *testing.T) {
    mockRepo := &MockProviderRepo{
        existsByNameFn: func(name string) (bool, error) {
            return false, nil
        },
        createFn: func(provider *model.Provider) error {
            provider.ID = uuid.New()
            return nil
        },
    }

    svc := NewProviderService(mockRepo, nil)
    result, err := svc.CreateProvider(input)
    
    assert.NoError(t, err)
    assert.NotNil(t, result)
}
```

### Testing Error Cases
```go
func TestCreateProvider_NameExists(t *testing.T) {
    mockRepo := &MockProviderRepo{
        existsByNameFn: func(name string) (bool, error) {
            return true, nil
        },
    }

    svc := NewProviderService(mockRepo, nil)
    result, err := svc.CreateProvider(input)
    
    assert.Error(t, err)
    assert.Nil(t, result)
    assert.True(t, errors.Is(err, ErrProviderNameExists))
}
```

### Testing Validation
```go
func TestCreateProvider_InvalidBody(t *testing.T) {
    router := gin.New()
    handler := NewProviderHandler(mockSvc)
    router.POST("/api/v1/providers", handler.CreateProvider)

    w := httptest.NewRecorder()
    req := httptest.NewRequest("POST", "/api/v1/providers",
        bytes.NewBufferString(`{"name":""}`))
    req.Header.Set("Content-Type", "application/json")

    router.ServeHTTP(w, req)
    
    assert.Equal(t, http.StatusBadRequest, w.Code)
}
```

## Test Coverage Areas

### Provider Management
- ✓ Create provider (success, name conflict, validation)
- ✓ List providers (pagination, empty list, errors)
- ✓ Get provider (success, not found, invalid ID)
- ✓ Update provider (success, not found, name conflict)
- ✓ Toggle status (enable/disable, cascade to models)
- ✓ Delete provider (success, not found, has models)

### Model Tags
- ✓ List tags (success, empty, errors)
- ✓ Create tag (success, conflict, validation)
- ✓ Update tag (success, not found, validation)
- ✓ Delete tag (success, invalid ID)
- ✓ Set model tags (success, invalid ID, cascade)
- ✓ Get model tags (success, empty, errors)

### Model Sync
- ✓ Sync models (success, unsupported, provider disabled)
- ✓ Confirm sync (register, skip, bulk operations)
- ✓ Error handling (provider not found, upstream errors)

### Middleware
- ✓ Request ID (generation, propagation, context)
- ✓ JWT Auth (valid token, missing, invalid format)
- ✓ Internal Auth (valid token, missing, empty)
- ✓ Logger (skip paths, error responses)
- ✓ Middleware chaining (order dependency, interaction)

## Assertion Patterns

### Using stretchr/testify

```go
import "github.com/stretchr/testify/assert"

// Basic assertions
assert.NoError(t, err)
assert.Error(t, err)
assert.Nil(t, value)
assert.NotNil(t, value)
assert.Equal(t, expected, actual)
assert.NotEqual(t, unexpected, actual)

// String assertions
assert.Contains(t, response, "error")
assert.NotContains(t, response, "success")

// Error type assertions
assert.True(t, errors.Is(err, ErrProviderNotFound))

// Require (fails test immediately)
require.NoError(t, err)
require.NotNil(t, result)
```

## Debugging Tests

### Verbose Output
```bash
go test ./internal/controlplane/... -v
```

### Print Debug Information
```go
t.Logf("Debug info: %v", variable)
```

### Run Single Test with Debugging
```bash
go test -run TestCreateProvider_Success -v ./internal/controlplane/handler/
```

### Test Coverage Analysis
```bash
go test ./internal/controlplane/... -cover
go test ./internal/controlplane/... -coverprofile=coverage.out
go tool cover -html=coverage.out  # Opens browser with coverage report
```

## Adding New Tests

### Step 1: Create Test Function
```go
func TestNewFeature_Success(t *testing.T) {
    // Setup mocks
    mockRepo := &MockProviderRepo{
        // configure mock behavior
    }
    
    // Create service
    svc := NewProviderService(mockRepo, nil)
    
    // Execute
    result, err := svc.NewFeature()
    
    // Assert
    assert.NoError(t, err)
    assert.NotNil(t, result)
}
```

### Step 2: Follow Naming Convention
- `TestFeature_Success` - Happy path
- `TestFeature_Error` - Error cases
- `TestFeature_Validation` - Input validation
- `TestFeature_EdgeCase` - Boundary conditions

### Step 3: Use Table-Driven for Multiple Scenarios
```go
tests := []struct {
    name    string
    input   string
    wantErr bool
}{
    {"valid", "good", false},
    {"invalid", "bad", true},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test
    })
}
```

## Best Practices

1. **Isolation**: Each test should be independent
2. **Clarity**: Test names should describe what's being tested
3. **Setup/Teardown**: Use helpers for common setup
4. **Mocking**: Mock only external dependencies
5. **Assertions**: Use clear, specific assertions
6. **Coverage**: Aim for >80% code coverage
7. **Performance**: Keep tests fast (<100ms each)
8. **Readability**: Arrange tests in AAA pattern (Arrange, Act, Assert)

## Common Issues & Solutions

### Issue: Import Cycles
**Solution**: Ensure mocks are in `*_test.go` files

### Issue: Flaky Tests
**Solution**: Don't use time.Sleep(), use proper mocking

### Issue: Database Connection Errors
**Solution**: All tests use mocks, no database needed

### Issue: Port Conflicts
**Solution**: httptest allocates free ports automatically

### Issue: Timing Issues
**Solution**: Use mocks with controlled behavior, not real async operations

## CI/CD Integration

### GitHub Actions Example
```yaml
- name: Run Tests
  run: go test ./internal/controlplane/... -v -cover

- name: Generate Coverage Report
  run: go test ./internal/controlplane/... -coverprofile=coverage.out

- name: Upload Coverage
  uses: codecov/codecov-action@v2
  with:
    files: ./coverage.out
```

## Performance Guidelines

- Single test: <10ms (target)
- Full test suite: <5s (target)
- Handler tests: ~1-2ms each
- Service tests: ~0.5-1ms each
- Middleware tests: ~0.1-0.5ms each

## References

- [Go Testing Package](https://pkg.go.dev/testing)
- [Gin Testing](https://github.com/gin-gonic/gin#testing)
- [Testify](https://github.com/stretchr/testify)
- [httptest Package](https://pkg.go.dev/net/http/httptest)

## Support

For issues or questions:
1. Check the TEST_SUMMARY.md for detailed test documentation
2. Review existing tests for patterns
3. Refer to Go testing best practices
4. Check Gin and testify documentation
