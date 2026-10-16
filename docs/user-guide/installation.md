# Installation

## Binary download

Download the latest pre-built binary from the [releases page](https://github.com/TomzxCode/ghx/releases/tag/latest).

=== "darwin-amd64"
    ```bash
    curl -LO https://github.com/TomzxCode/ghx/releases/latest/download/ghx-darwin-amd64
    chmod +x ghx-darwin-amd64
    mv ghx-darwin-amd64 /usr/local/bin/ghx
    ```

=== "darwin-arm64"
    ```bash
    curl -LO https://github.com/TomzxCode/ghx/releases/latest/download/ghx-darwin-arm64
    chmod +x ghx-darwin-arm64
    mv ghx-darwin-arm64 /usr/local/bin/ghx
    ```

=== "linux-amd64"
    ```bash
    curl -LO https://github.com/TomzxCode/ghx/releases/latest/download/ghx-linux-amd64
    chmod +x ghx-linux-amd64
    mv ghx-linux-amd64 /usr/local/bin/ghx
    ```

=== "windows-amd64"
    ```powershell
    curl -LO https://github.com/TomzxCode/ghx/releases/latest/download/ghx-windows-amd64.exe
    rename-item ghx-windows-amd64.exe ghx.exe
    ```

## Build from source

Requires [Go](https://go.dev/) 1.26 or later.

```bash
go install github.com/tomzxcode/ghx@main
```

This places the binary at `$(go env GOPATH)/bin/ghx`. Make sure that directory is on your `PATH`.

## Verify

```bash
ghx --version
```

## Shell completion

ghx can generate completion scripts for the common shells:

```bash
ghx completion bash        # also: zsh, fish, powershell
```

Each subcommand prints the completion script for that shell; see `ghx completion <shell> --help` for where to install it. Pass `--no-descriptions` to omit completion descriptions.

## Getting help

`ghx help [command]` prints the full help for any command (the same output as `ghx <command> --help`).
