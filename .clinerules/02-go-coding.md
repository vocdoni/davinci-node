## Go Code Standards

### Error Handling
- Prefer `fmt.Errorf()` for error creation (not `errors.New()`) - this is a project-specific pattern
- Always wrap errors with context using `fmt.Errorf("message: %w", err)`
