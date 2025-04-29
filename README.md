# Ryegen
Create a Rye interpreter with automatic bindings for a Go library.

## Create a binding (requires go1.24+)
### 1. Initialize a new module:
- Create a new directory as you would for any new Go module.
- Open a command line inside the directory you just created.
- Initialize the Go module, e.g.: `go mod init my-module`
- Install Ryegen as a tool: `go get -tool github.com/refaktor/ryegen/v2@main`
- Ignore Ryegen module cache in git: `echo /ryegen_src > .gitignore`

### 2. Create a file named `generate.go`:
`generate.go`:
```go
package main

//go:generate go tool ryegen example.com/module@latest
```
Replace `example.com/module` with any Go module path. You may also replace `latest` with any other version.

### 3. Run ryegen
```
go generate .
```

Re-run go generate any time you want to re-generate the bindings.

## Run example (binding for fyne v2.6.0)
```
cd _examples/fyne
go generate .
go run . ./example.rye
```