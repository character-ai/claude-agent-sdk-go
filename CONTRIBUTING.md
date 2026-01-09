# Contributing

Contributions are welcome! Here's how to get started.

## Development Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/character-tech/claude-agent-sdk-go.git
   cd claude-agent-sdk-go
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Ensure Claude Code CLI is installed and authenticated.

## Running Examples

```bash
go run examples/simple/main.go
go run examples/streaming/main.go
go run examples/tools/main.go
```

## Code Style

- Follow standard Go conventions and `gofmt`
- Use meaningful variable and function names
- Add comments for exported functions and types
- Keep functions focused and small

## Pull Requests

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Make your changes
4. Run tests: `go test ./...`
5. Run linter: `go vet ./...`
6. Commit with a clear message
7. Push and open a pull request

## Commit Messages

Use clear, descriptive commit messages:

```
Add support for custom tool timeouts

- Add Timeout field to ToolDefinition
- Update RegisterFunc to handle context deadlines
- Add example showing timeout usage
```

## Reporting Issues

When reporting bugs, please include:

- Go version (`go version`)
- Claude Code CLI version (`claude --version`)
- Operating system
- Steps to reproduce
- Expected vs actual behavior

## Questions

Open an issue for questions or discussions about the SDK.
